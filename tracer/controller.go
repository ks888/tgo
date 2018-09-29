package tracer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ks888/tgo/debugapi"
	"github.com/ks888/tgo/tracee"
)

// ErrInterrupted indicates the tracer is interrupted due to the Interrupt() call.
var ErrInterrupted = errors.New("interrupted")

// Controller controls the associated tracee process.
type Controller struct {
	process     *tracee.Process
	statusStore map[int]goRoutineStatus

	tracingPoint *tracingPoint
	hitOnce      bool

	interrupted bool
	// The traced data is written to this writer.
	outputWriter io.Writer
}

type goRoutineStatus struct {
	usedStackSize uint64
	// callingFunctions is the list of functions in the call stack.
	// This list include only the functions which hit the breakpoint before and so is not complete.
	callingFunctions []*tracee.Function
}

// NewController returns the new controller.
func NewController() *Controller {
	return &Controller{outputWriter: os.Stdout}
}

// LaunchTracee launches the new tracee process to be controlled.
func (c *Controller) LaunchTracee(name string, arg ...string) error {
	var err error
	c.statusStore = make(map[int]goRoutineStatus)
	c.process, err = tracee.LaunchProcess(name, arg...)
	return err
}

// AttachTracee attaches to the existing process.
func (c *Controller) AttachTracee(pid int) error {
	var err error
	c.statusStore = make(map[int]goRoutineStatus)
	c.process, err = tracee.AttachProcess(pid)
	return err
}

// SetTracingPoint sets the starting point of the tracing. The tracing is enabled when this function is called and disabled when returned.
// The tracing point can be set only once.
func (c *Controller) SetTracingPoint(functionName string) error {
	if c.tracingPoint != nil {
		return errors.New("tracing point is set already")
	}

	function, err := c.findFunction(functionName)
	if err != nil {
		return err
	}

	if !c.canSetBreakpoint(function) {
		return fmt.Errorf("can't set the tracing point for %s", functionName)
	}

	if err := c.process.SetBreakpoint(function.Value); err != nil {
		return err
	}

	c.tracingPoint = &tracingPoint{function: function}
	return nil
}

func (c *Controller) findFunction(functionName string) (*tracee.Function, error) {
	functions, err := c.process.Binary.ListFunctions()
	if err != nil {
		return nil, err
	}

	for _, function := range functions {
		if function.Name == functionName {
			return function, nil
		}
	}
	return nil, errors.New("failed to find function")
}

func (c *Controller) canSetBreakpoint(function *tracee.Function) bool {
	// TODO: too conservative. At least funcs to operate map, chan, slice should be allowed.
	if strings.HasPrefix(function.Name, "runtime") && !function.IsExported() {
		return false
	}
	prefixesToAvoid := []string{"_rt0", "type."}
	for _, prefix := range prefixesToAvoid {
		if strings.HasPrefix(function.Name, prefix) {
			return false
		}
	}
	return true
}

// MainLoop repeatedly lets the tracee continue and then wait an event.
func (c *Controller) MainLoop() error {
	trappedThreadIDs, event, err := c.process.ContinueAndWait()
	if err != nil {
		return err
	}

	for {
		switch event.Type {
		case debugapi.EventTypeExited:
			return nil
		case debugapi.EventTypeCoreDump:
			return errors.New("the process exited due to core dump")
		case debugapi.EventTypeTerminated:
			return fmt.Errorf("the process exited due to signal %d", event.Data)
		case debugapi.EventTypeTrapped:
			trappedThreadIDs, event, err = c.handleTrapEvent(trappedThreadIDs)
			if err == ErrInterrupted {
				return err
			} else if err != nil {
				return fmt.Errorf("failed to handle trap event: %v", err)
			}
		default:
			return fmt.Errorf("unknown event: %v", event.Type)
		}
	}
}

func (c *Controller) handleTrapEvent(trappedThreadIDs []int) ([]int, debugapi.Event, error) {
	for _, threadID := range trappedThreadIDs {
		if err := c.handleTrapEventOfThread(threadID); err != nil {
			return nil, debugapi.Event{}, err
		}
	}
	return c.process.ContinueAndWait()
}

func (c *Controller) handleTrapEventOfThread(threadID int) error {
	goRoutineInfo, err := c.process.CurrentGoRoutineInfo(threadID)
	if err != nil {
		return err
	}
	goRoutineID := int(goRoutineInfo.ID)

	if !c.process.HitBreakpoint(goRoutineInfo.CurrentPC-1, goRoutineID) {
		return c.handleTrapAtUnrelatedBreakpoint(threadID, goRoutineInfo)
	}

	status, _ := c.statusStore[goRoutineID]
	if goRoutineInfo.UsedStackSize < status.usedStackSize {
		return c.handleTrapAtFunctionReturn(threadID, status, goRoutineInfo)
	} else if goRoutineInfo.UsedStackSize == status.usedStackSize {
		// it's likely we are in the same stack frame as before (typical for the stack growth case).
		return c.handleTrapAtUnrelatedBreakpoint(threadID, goRoutineInfo)
	}
	return c.handleTrapAtFunctionCall(threadID, status, goRoutineInfo)
}

func (c *Controller) handleTrapAtUnrelatedBreakpoint(threadID int, goRoutineInfo tracee.GoRoutineInfo) error {
	trappedAddr := goRoutineInfo.CurrentPC - 1

	if err := c.process.SetPC(threadID, trappedAddr); err != nil {
		return err
	}

	if err := c.process.ClearBreakpoint(trappedAddr); err != nil {
		return err
	}

	if _, _, err := c.process.StepAndWait(threadID); err != nil {
		return err
	}

	return c.process.SetBreakpoint(trappedAddr)
}

func (c *Controller) handleTrapAtFunctionCall(threadID int, status goRoutineStatus, goRoutineInfo tracee.GoRoutineInfo) error {
	if c.tracingPoint.Hit(goRoutineInfo.CurrentPC - 1) {
		if !c.hitOnce {
			if err := c.setBreakpointsExceptTracingPoint(); err != nil {
				return err
			}
			c.hitOnce = true
		}

		c.tracingPoint.Enter(goRoutineInfo.ID, goRoutineInfo.UsedStackSize)
	}

	stackFrame, err := c.currentStackFrame(goRoutineInfo)
	if err != nil {
		return err
	}

	goRoutineID := int(goRoutineInfo.ID)
	if c.tracingPoint.Inside(goRoutineInfo.ID) {
		if err := c.printFunctionInput(goRoutineID, stackFrame, len(status.callingFunctions)+1); err != nil {
			return err
		}
	}

	if err := c.process.SetConditionalBreakpoint(stackFrame.ReturnAddress, goRoutineID); err != nil {
		return err
	}

	funcAddr := stackFrame.Function.Value
	if err := c.process.SetPC(threadID, funcAddr); err != nil {
		return err
	}

	if err := c.process.ClearBreakpoint(funcAddr); err != nil {
		return err
	}

	if c.interrupted {
		if err := c.process.Detach(); err != nil {
			return err
		}
		return ErrInterrupted
	}

	if _, _, err := c.process.StepAndWait(threadID); err != nil {
		return err
	}

	if err := c.process.SetBreakpoint(funcAddr); err != nil {
		return err
	}

	c.statusStore[goRoutineID] = goRoutineStatus{
		usedStackSize:    goRoutineInfo.UsedStackSize,
		callingFunctions: append(status.callingFunctions, stackFrame.Function),
	}
	return nil
}

func (c *Controller) setBreakpointsExceptTracingPoint() error {
	functions, err := c.process.Binary.ListFunctions()
	if err != nil {
		return err
	}
	for _, function := range functions {
		if !c.canSetBreakpoint(function) || function.Name == c.tracingPoint.function.Name {
			continue
		}
		if err := c.process.SetBreakpoint(function.Value); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) handleTrapAtFunctionReturn(threadID int, status goRoutineStatus, goRoutineInfo tracee.GoRoutineInfo) error {
	goRoutineID := int(goRoutineInfo.ID)

	if c.tracingPoint.Inside(goRoutineInfo.ID) {
		function := status.callingFunctions[len(status.callingFunctions)-1]
		prevStackFrame, err := c.prevStackFrame(goRoutineInfo, function.Value)
		if err != nil {
			return err
		}
		if err := c.printFunctionOutput(goRoutineID, prevStackFrame, len(status.callingFunctions)); err != nil {
			return err
		}

		if c.tracingPoint.Hit(function.Value) {
			// assumes 'call' inst consumed 8-bytes to save the return address to stack.
			c.tracingPoint.Exit(goRoutineInfo.ID, goRoutineInfo.UsedStackSize+8)
		}
	}

	breakpointAddr := goRoutineInfo.CurrentPC - 1
	if err := c.process.SetPC(threadID, breakpointAddr); err != nil {
		return err
	}

	if err := c.process.ClearBreakpoint(breakpointAddr); err != nil {
		return err
	}

	c.statusStore[goRoutineID] = goRoutineStatus{
		callingFunctions: status.callingFunctions[0 : len(status.callingFunctions)-1],
		usedStackSize:    goRoutineInfo.UsedStackSize,
	}
	return nil
}

// It must be called at the beginning of the function, because it assumes rbp = rsp-8
func (c *Controller) currentStackFrame(goRoutineInfo tracee.GoRoutineInfo) (*tracee.StackFrame, error) {
	return c.process.StackFrameAt(goRoutineInfo.CurrentStackAddr-8, goRoutineInfo.CurrentPC)
}

// It must be called at return address, because it assumes rbp = rsp-16
func (c *Controller) prevStackFrame(goRoutineInfo tracee.GoRoutineInfo, rip uint64) (*tracee.StackFrame, error) {
	return c.process.StackFrameAt(goRoutineInfo.CurrentStackAddr-16, rip)
}

func (c *Controller) printFunctionInput(goRoutineID int, stackFrame *tracee.StackFrame, depth int) error {
	var args []string
	for _, arg := range stackFrame.InputArguments {
		var value string
		switch arg.Typ.String() {
		case "int", "int64":
			value = strconv.Itoa(int(binary.LittleEndian.Uint64(arg.Value)))
		default:
			value = fmt.Sprintf("%v", arg.Value)
		}
		args = append(args, fmt.Sprintf("%s = %s", arg.Name, value))
	}

	fmt.Fprintf(c.outputWriter, "%s=> (#%02d) %s(%s)\n", strings.Repeat(" ", depth-1), goRoutineID, stackFrame.Function.Name, strings.Join(args, ", "))

	return nil
}

func (c *Controller) printFunctionOutput(goRoutineID int, stackFrame *tracee.StackFrame, depth int) error {
	var args []string
	for _, arg := range stackFrame.OutputArguments {
		var value string
		switch arg.Typ.String() {
		case "int", "int64":
			value = strconv.Itoa(int(binary.LittleEndian.Uint64(arg.Value)))
		default:
			value = fmt.Sprintf("%v", arg.Value)
		}
		args = append(args, fmt.Sprintf("%s = %s", arg.Name, value))
	}
	fmt.Fprintf(c.outputWriter, "%s<= (#%02d) %s(...) (%s)\n", strings.Repeat(" ", depth-1), goRoutineID, stackFrame.Function.Name, strings.Join(args, ", "))

	return nil
}

// Interrupt interrupts the main loop.
func (c *Controller) Interrupt() {
	c.interrupted = true
}

type goRoutineInside struct {
	id int64
	// stackSize is the used stack size of the go routine when the tracing starts.
	stackSize uint64
}

type tracingPoint struct {
	function         *tracee.Function
	goRoutinesInside []goRoutineInside
}

// Hit returns true if pc is same as tracing point.
func (p *tracingPoint) Hit(pc uint64) bool {
	return pc == p.function.Value
}

// Enter updates the list of the go routines which are inside the tracing point.
// It does nothing if the go routine has already entered.
func (p *tracingPoint) Enter(goRoutineID int64, stackSize uint64) {
	for _, goRoutine := range p.goRoutinesInside {
		if goRoutine.id == goRoutineID {
			return
		}
	}

	p.goRoutinesInside = append(p.goRoutinesInside, goRoutineInside{id: goRoutineID, stackSize: stackSize})
	return
}

// Exit removes the go routine from the inside-go routines list.
// Note that the go routine is not removed if the stack size is different (to support recursive call's case).
func (p *tracingPoint) Exit(goRoutineID int64, stackSize uint64) bool {
	for i, goRoutine := range p.goRoutinesInside {
		if goRoutine.id == goRoutineID && goRoutine.stackSize == stackSize {
			p.goRoutinesInside = append(p.goRoutinesInside[0:i], p.goRoutinesInside[i+1:len(p.goRoutinesInside)]...)
			return true
		}
	}

	return false
}

// Inside returns true if the go routine is inside the tracing point.
func (p *tracingPoint) Inside(goRoutineID int64) bool {
	for _, goRoutine := range p.goRoutinesInside {
		if goRoutine.id == goRoutineID {
			return true
		}
	}
	return false
}
