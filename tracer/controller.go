package tracer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ks888/tgo/debugapi"
	"github.com/ks888/tgo/tracee"
)

var ErrInterrupted = errors.New("interrupted")

type Controller struct {
	process     *tracee.Process
	statusStore map[int]goRoutineStatus
	interrupted bool
}

type goRoutineStatus struct {
	statusType    goRoutineStatusType
	usedStackSize uint64
	// callingFunctions is the list of functions in the call stack.
	// This list include only the functions which hit the breakpoint before and so is not complete.
	callingFunctions []*tracee.Function
	// breakpointToRestore is the address the break point should be set, but temporarily cleared by the go routine for single stepping.
	// Usually the function doesn't change after the single stepping and so this address is not necessary,
	// but the function changes when the function 'CALL's at the beginning of the function.
	breakpointToRestore uint64
}

type goRoutineStatusType int

const (
	goRoutineRunning goRoutineStatusType = iota
	goRoutineSingleStepping
)

func (c *Controller) LaunchTracee(name string, arg ...string) error {
	var err error
	c.process, err = tracee.LaunchProcess(name, arg...)
	if err != nil {
		return err
	}
	c.statusStore = make(map[int]goRoutineStatus)

	functions, err := c.process.Binary.ListFunctions()
	if err != nil {
		return err
	}
	for _, function := range functions {
		if !c.canSetBreakpoint(function) {
			continue
		}
		if err := c.process.SetBreakpoint(function.Value); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) canSetBreakpoint(function *tracee.Function) bool {
	// may be too conservative, but need to understand runtime more to correctly set breakpoints to non-exported functions.
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

func (c *Controller) MainLoop() error {
	event, err := c.process.ContinueAndWait()
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
			event, err = c.handleTrapEvent()
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

func (c *Controller) handleTrapEvent() (debugapi.Event, error) {
	goRoutineInfo, err := c.process.CurrentGoRoutineInfo()
	if err != nil {
		return debugapi.Event{}, err
	}
	goRoutineID := int(goRoutineInfo.ID)

	status, ok := c.statusStore[goRoutineID]
	if !ok {
		status = goRoutineStatus{statusType: goRoutineRunning}
	}

	switch status.statusType {
	case goRoutineRunning:
		if !c.process.HitBreakpoint(goRoutineInfo.CurrentPC-1, goRoutineID) {
			return c.handleTrapAtUnrelatedBreakpoint(status, goRoutineInfo)
		}

		if goRoutineInfo.UsedStackSize < status.usedStackSize {
			return c.handleTrapAtFunctionReturn(status, goRoutineInfo)
		} else if goRoutineInfo.UsedStackSize == status.usedStackSize {
			// it's likely we are in the same stack frame as before (typical for the stack growth case).
			return c.handleTrapAtUnrelatedBreakpoint(status, goRoutineInfo)
		}
		return c.handleTrapAtFunctionCall(status, goRoutineInfo)

	case goRoutineSingleStepping:
		if err := c.process.SetBreakpoint(status.breakpointToRestore); err != nil {
			return debugapi.Event{}, err
		}

		status.statusType = goRoutineRunning
		status.breakpointToRestore = 0
		c.statusStore[goRoutineID] = status
		return c.process.ContinueAndWait()

	default:
		return debugapi.Event{}, fmt.Errorf("unknown status: %v", status.statusType)
	}
}

func (c *Controller) handleTrapAtUnrelatedBreakpoint(status goRoutineStatus, goRoutineInfo tracee.GoRoutineInfo) (debugapi.Event, error) {
	goRoutineID := int(goRoutineInfo.ID)
	trappedAddr := goRoutineInfo.CurrentPC - 1

	if err := c.process.SetPC(trappedAddr); err != nil {
		return debugapi.Event{}, err
	}

	if err := c.process.ClearBreakpoint(trappedAddr); err != nil {
		return debugapi.Event{}, err
	}

	c.statusStore[goRoutineID] = goRoutineStatus{
		statusType:          goRoutineSingleStepping,
		usedStackSize:       goRoutineInfo.UsedStackSize,
		callingFunctions:    status.callingFunctions,
		breakpointToRestore: trappedAddr,
	}
	return c.process.StepAndWait()
}

func (c *Controller) handleTrapAtFunctionCall(status goRoutineStatus, goRoutineInfo tracee.GoRoutineInfo) (debugapi.Event, error) {
	stackFrame, err := c.currentStackFrame(goRoutineInfo)
	if err != nil {
		return debugapi.Event{}, err
	}

	goRoutineID := int(goRoutineInfo.ID)
	funcAddr := stackFrame.Function.Value
	if err := c.printFunctionInput(goRoutineID, stackFrame, len(status.callingFunctions)+1); err != nil {
		return debugapi.Event{}, err
	}

	if err := c.process.SetConditionalBreakpoint(stackFrame.ReturnAddress, goRoutineID); err != nil {
		return debugapi.Event{}, err
	}

	if err := c.process.SetPC(funcAddr); err != nil {
		return debugapi.Event{}, err
	}

	if err := c.process.ClearBreakpoint(funcAddr); err != nil {
		return debugapi.Event{}, err
	}

	if c.interrupted {
		if err := c.process.Detach(); err != nil {
			return debugapi.Event{}, err
		}
		return debugapi.Event{}, ErrInterrupted
	}

	c.statusStore[goRoutineID] = goRoutineStatus{
		statusType:          goRoutineSingleStepping,
		usedStackSize:       goRoutineInfo.UsedStackSize,
		callingFunctions:    append(status.callingFunctions, stackFrame.Function),
		breakpointToRestore: funcAddr,
	}
	return c.process.StepAndWait()
}

func (c *Controller) handleTrapAtFunctionReturn(status goRoutineStatus, goRoutineInfo tracee.GoRoutineInfo) (debugapi.Event, error) {
	goRoutineID := int(goRoutineInfo.ID)

	function := status.callingFunctions[len(status.callingFunctions)-1]
	prevStackFrame, err := c.prevStackFrame(goRoutineInfo, function.Value)
	if err != nil {
		return debugapi.Event{}, err
	}
	if err := c.printFunctionOutput(goRoutineID, prevStackFrame, len(status.callingFunctions)); err != nil {
		return debugapi.Event{}, err
	}

	breakpointAddr := goRoutineInfo.CurrentPC - 1
	if err := c.process.SetPC(breakpointAddr); err != nil {
		return debugapi.Event{}, err
	}

	if err := c.process.ClearBreakpoint(breakpointAddr); err != nil {
		return debugapi.Event{}, err
	}

	c.statusStore[goRoutineID] = goRoutineStatus{
		statusType:       goRoutineRunning,
		callingFunctions: status.callingFunctions[0 : len(status.callingFunctions)-1],
		usedStackSize:    goRoutineInfo.UsedStackSize,
	}
	return c.process.ContinueAndWait()
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
	fmt.Printf("%s => (#%02d) %s(%s)\n", strings.Repeat(" ", depth), goRoutineID, stackFrame.Function.Name, strings.Join(args, ", "))

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
	fmt.Printf("%s<= (#%02d) %s(...) (%s)\n", strings.Repeat(" ", depth-1), goRoutineID, stackFrame.Function.Name, strings.Join(args, ", "))

	return nil
}

// Interrupt has the main loop exit.
func (c *Controller) Interrupt() {
	c.interrupted = true
}
