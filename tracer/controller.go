package tracer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ks888/tgo/debugapi"
	"github.com/ks888/tgo/tracee"
	"golang.org/x/arch/x86/x86asm"
)

const chanBufferSize = 64

// ErrInterrupted indicates the tracer is interrupted due to the Interrupt() call.
var ErrInterrupted = errors.New("interrupted")

type breakpointHint int

const (
	// These hints are used to determine how to handle a go routine which hit a breakpoint.
	// No need to cover all the breakpoints.
	breakpointHintUnknown breakpointHint = iota
	breakpointHintCall
	breakpointHintDeferredFunc
)

// Controller controls the associated tracee process.
type Controller struct {
	process             *tracee.Process
	firstModuleDataAddr uint64
	statusStore         map[int64]goRoutineStatus
	callInstAddrCache   map[uint64][]uint64

	breakpointHints map[uint64]breakpointHint
	breakpoints     Breakpoints

	tracingPoints     tracingPoints
	tracingGoRoutines tracingGoRoutines
	traceLevel        int
	parseLevel        int

	// Use the buffered channels to handle the requests to the controller asyncronously.
	// It's because the tracee process must be trapped to handle these requests, but the process may not
	// be trapped when the requests are sent.
	interruptCh            chan bool
	pendingStartTracePoint chan uint64
	pendingEndTracePoint   chan uint64
	// The traced data is written to this writer.
	outputWriter io.Writer
}

type goRoutineStatus struct {
	// This list include only the functions which hit the breakpoint before and so is not complete.
	callingFunctions []callingFunction
}

func (status goRoutineStatus) usedStackSize() uint64 {
	if len(status.callingFunctions) > 0 {
		return status.callingFunctions[len(status.callingFunctions)-1].usedStackSize
	}

	return 0
}

type callingFunction struct {
	*tracee.Function
	returnAddress          uint64
	usedStackSize          uint64
	setCallInstBreakpoints bool
}

// NewController returns the new controller.
func NewController() *Controller {
	return &Controller{
		outputWriter:           os.Stdout,
		statusStore:            make(map[int64]goRoutineStatus),
		breakpointHints:        make(map[uint64]breakpointHint),
		callInstAddrCache:      make(map[uint64][]uint64),
		interruptCh:            make(chan bool, chanBufferSize),
		pendingStartTracePoint: make(chan uint64, chanBufferSize),
		pendingEndTracePoint:   make(chan uint64, chanBufferSize),
	}
}

// Attributes represents the tracee's attributes.
type Attributes tracee.Attributes

// LaunchTracee launches the new tracee process to be controlled.
func (c *Controller) LaunchTracee(name string, arg []string, attrs Attributes) error {
	var err error
	c.process, err = tracee.LaunchProcess(name, arg, tracee.Attributes(attrs))
	c.breakpoints = NewBreakpoints(c.process.SetBreakpoint, c.process.ClearBreakpoint)
	return err
}

// AttachTracee attaches to the existing process.
func (c *Controller) AttachTracee(pid int, attrs Attributes) error {
	var err error
	c.process, err = tracee.AttachProcess(pid, tracee.Attributes(attrs))
	c.breakpoints = NewBreakpoints(c.process.SetBreakpoint, c.process.ClearBreakpoint)
	return err
}

// AddStartTracePoint adds the starting point of the tracing. The go routines which passed one of the starting points before are traced.
func (c *Controller) AddStartTracePoint(startAddr uint64) error {
	select {
	case c.pendingStartTracePoint <- startAddr:
	default:
		// maybe buffer full
		return errors.New("failed to add start trace point")
	}
	return nil
}

// AddEndTracePoint adds the ending point of the tracing. The go routines which passed one of the ending points are not traced anymore.
func (c *Controller) AddEndTracePoint(endAddr uint64) error {
	select {
	case c.pendingEndTracePoint <- endAddr:
	default:
		// maybe buffer full
		return errors.New("failed to add end trace point")
	}
	return nil
}

// SetTraceLevel set the tracing level, which determines whether to print the traced info of the functions.
// The traced info is printed if the function is (directly or indirectly) called by the trace point function AND
// the stack depth is within the `level`.
// The depth here is the relative value from the point the tracing starts.
func (c *Controller) SetTraceLevel(level int) {
	c.traceLevel = level
}

// SetParseLevel sets the parsing level, which determines how deeply the parser parses the value of args.
func (c *Controller) SetParseLevel(level int) {
	c.parseLevel = level
}

// MainLoop repeatedly lets the tracee continue and then wait an event. It returns ErrInterrupted error if
// the trace ends due to the interrupt.
func (c *Controller) MainLoop() error {
	defer c.process.Detach() // the connection status is unknown at this point

	event, err := c.continueAndWait()
	if err == ErrInterrupted {
		return err
	} else if err != nil {
		return fmt.Errorf("failed to trace: %v", err)
	}

	for {
		switch event.Type {
		case debugapi.EventTypeExited:
			return nil
		case debugapi.EventTypeCoreDump:
			return errors.New("the process exited due to core dump")
		case debugapi.EventTypeTerminated:
			return fmt.Errorf("the process exited due to signal %d", event.Data.(int))
		case debugapi.EventTypeTrapped:
			trappedThreadIDs := event.Data.([]int)
			event, err = c.handleTrapEvent(trappedThreadIDs)
			if err == ErrInterrupted {
				return err
			} else if err != nil {
				return fmt.Errorf("failed to trace: %v", err)
			}
		default:
			return fmt.Errorf("unknown event: %v", event.Type)
		}
	}
}

// continueAndWait resumes the traced process and waits the process trapped again.
// It handles requests via channels before resuming.
func (c *Controller) continueAndWait() (debugapi.Event, error) {
	select {
	case <-c.interruptCh:
		return debugapi.Event{}, ErrInterrupted
	default:
		if err := c.setPendingTracePoints(); err != nil {
			return debugapi.Event{}, err
		}

		return c.process.ContinueAndWait()
	}
}

func (c *Controller) setPendingTracePoints() error {
	for {
		select {
		case startAddr := <-c.pendingStartTracePoint:
			if c.tracingPoints.IsStartAddress(startAddr) {
				continue // set already
			}

			if err := c.breakpoints.Set(startAddr); err != nil {
				return err
			}
			c.tracingPoints.startAddressList = append(c.tracingPoints.startAddressList, startAddr)

		case endAddr := <-c.pendingEndTracePoint:
			if c.tracingPoints.IsEndAddress(endAddr) {
				continue // set already
			}

			if err := c.breakpoints.Set(endAddr); err != nil {
				return err
			}
			c.tracingPoints.endAddressList = append(c.tracingPoints.endAddressList, endAddr)

		default:
			return nil // no data
		}
	}
}

func (c *Controller) handleTrapEvent(trappedThreadIDs []int) (debugapi.Event, error) {
	for i := 0; i < len(trappedThreadIDs); i++ {
		threadID := trappedThreadIDs[i]
		if err := c.handleTrapEventOfThread(threadID); err != nil {
			return debugapi.Event{}, fmt.Errorf("failed to handle trap event (thread id: %d): %v", threadID, err)
		}
	}

	return c.continueAndWait()
}

func (c *Controller) handleTrapEventOfThread(threadID int) error {
	goRoutineInfo, err := c.process.CurrentGoRoutineInfo(threadID)
	if err != nil || goRoutineInfo.ID == 0 {
		return c.handleTrappedSystemRoutine(threadID)
	}

	breakpointAddr := goRoutineInfo.CurrentPC - 1
	if !c.breakpoints.Hit(breakpointAddr, goRoutineInfo.ID) {
		return c.handleTrapAtUnrelatedBreakpoint(threadID, breakpointAddr)
	}

	if err := c.updateTracingStatus(threadID, goRoutineInfo, breakpointAddr); err != nil {
		return err
	}

	if !c.tracingGoRoutines.Tracing(goRoutineInfo.ID) {
		return c.handleTrapAtUnrelatedBreakpoint(threadID, breakpointAddr)
	}

	status, _ := c.statusStore[goRoutineInfo.ID]
	if status.usedStackSize() > goRoutineInfo.UsedStackSize {
		if err := c.handleTrapAfterFunctionReturn(threadID, goRoutineInfo); err != nil {
			return err
		}
	}

	switch c.breakpointHints[breakpointAddr] {
	case breakpointHintCall:
		return c.handleTrapBeforeFunctionCall(threadID, goRoutineInfo)
	case breakpointHintDeferredFunc:
		return c.handleTrapAtDeferredFuncCall(threadID, goRoutineInfo)
	default:
		return c.handleTrapAtUnrelatedBreakpoint(threadID, breakpointAddr)
	}
}

func (c *Controller) updateTracingStatus(threadID int, goRoutineInfo tracee.GoRoutineInfo, breakpointAddr uint64) error {
	if c.tracingPoints.IsStartAddress(breakpointAddr) {
		if err := c.enterTracepoint(threadID, goRoutineInfo); err != nil {
			return err
		}
	}
	if c.tracingPoints.IsEndAddress(breakpointAddr) {
		return c.exitTracepoint(threadID, goRoutineInfo.ID, breakpointAddr)
	}
	return nil
}

func (c *Controller) enterTracepoint(threadID int, goRoutineInfo tracee.GoRoutineInfo) error {
	goRoutineID := goRoutineInfo.ID

	if err := c.setCallInstBreakpoints(goRoutineID, goRoutineInfo.CurrentPC); err != nil {
		return err
	}

	c.tracingGoRoutines.Add(goRoutineID)
	return nil
}

func (c *Controller) exitTracepoint(threadID int, goRoutineID int64, breakpointAddr uint64) error {
	c.tracingGoRoutines.Remove(goRoutineID)

	if !c.tracingGoRoutines.Tracing(goRoutineID) {
		if err := c.breakpoints.ClearAllByGoRoutineID(goRoutineID); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) setCallInstBreakpoints(goRoutineID int64, pc uint64) error {
	return c.alterCallInstBreakpoints(true, goRoutineID, pc)
}

func (c *Controller) clearCallInstBreakpoints(goRoutineID int64, pc uint64) error {
	return c.alterCallInstBreakpoints(false, goRoutineID, pc)
}

func (c *Controller) alterCallInstBreakpoints(enable bool, goRoutineID int64, pc uint64) error {
	f, err := c.process.FindFunction(pc)
	if err != nil {
		return err
	}

	callInstAddresses, err := c.findCallInstAddresses(f)
	if err != nil {
		return err
	}

	for _, callInstAddr := range callInstAddresses {
		if enable {
			err = c.breakpoints.SetConditional(callInstAddr, goRoutineID)
			c.breakpointHints[callInstAddr] = breakpointHintCall
		} else {
			err = c.breakpoints.ClearConditional(callInstAddr, goRoutineID)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) handleTrappedSystemRoutine(threadID int) error {
	threadInfo, err := c.process.CurrentThreadInfo(threadID)
	if err != nil {
		return err
	}

	breakpointAddr := threadInfo.CurrentPC - 1
	return c.process.SingleStep(threadID, breakpointAddr)
}

func (c *Controller) handleTrapAtUnrelatedBreakpoint(threadID int, breakpointAddr uint64) error {
	return c.process.SingleStep(threadID, breakpointAddr)
}

func (c *Controller) handleTrapBeforeFunctionCall(threadID int, goRoutineInfo tracee.GoRoutineInfo) error {
	if err := c.process.SingleStep(threadID, goRoutineInfo.CurrentPC-1); err != nil {
		return err
	}

	// Now the go routine jumped to the beginning of the function.
	goRoutineInfo, err := c.process.CurrentGoRoutineInfo(threadID)
	if err != nil {
		return err
	}

	if err := c.updateTracingStatus(threadID, goRoutineInfo, goRoutineInfo.CurrentPC); err != nil {
		return err
	}

	if !c.tracingGoRoutines.Tracing(goRoutineInfo.ID) {
		return c.handleTrapAtUnrelatedBreakpoint(threadID, goRoutineInfo.CurrentPC)
	}

	return c.handleTrapAtFunctionCall(threadID, goRoutineInfo.CurrentPC, goRoutineInfo)
}

// handleTrapAtFunctionCall handles the trapped event at the function call.
// It needs `breakpointAddr` though it's usually same as the function's start address.
// It is because some function, such as runtime.duffzero, directly jumps to the middle of the function and
// the breakpoint address is not explicit in that case.
func (c *Controller) handleTrapAtFunctionCall(threadID int, breakpointAddr uint64, goRoutineInfo tracee.GoRoutineInfo) error {
	stackFrame, err := c.currentStackFrame(goRoutineInfo)
	if err != nil {
		return err
	}

	// unwinded here in some cases:
	// * just recovered from panic.
	// * the last function used 'JMP' to call the next function and didn't change the SP. e.g. runtime.deferreturn
	remainingFuncs, _, err := c.unwindFunctions(goRoutineInfo, goRoutineInfo.UsedStackSize)
	if err != nil {
		return err
	}

	currStackDepth := len(remainingFuncs) + 1 // add the currently calling function
	callingFunc := callingFunction{
		Function:               stackFrame.Function,
		returnAddress:          stackFrame.ReturnAddress,
		usedStackSize:          goRoutineInfo.UsedStackSize,
		setCallInstBreakpoints: currStackDepth < c.traceLevel,
	}
	if err = c.addFunction(callingFunc, goRoutineInfo.ID); err != nil {
		return err
	}

	if currStackDepth <= c.traceLevel && c.printableFunc(stackFrame.Function) {
		if err := c.printFunctionInput(goRoutineInfo.ID, stackFrame, currStackDepth); err != nil {
			return err
		}
	}

	return c.process.SingleStep(threadID, breakpointAddr)
}

func (c *Controller) unwindFunctions(goRoutineInfo tracee.GoRoutineInfo, currUsedStackSize uint64) ([]callingFunction, []callingFunction, error) {
	remainingFuncs, unwindedFuncs, err := c.doUnwindFunctions(goRoutineInfo, currUsedStackSize)
	if err != nil {
		return nil, nil, err
	}

	c.statusStore[goRoutineInfo.ID] = goRoutineStatus{callingFunctions: remainingFuncs}
	return remainingFuncs, unwindedFuncs, nil
}

func (c *Controller) doUnwindFunctions(goRoutineInfo tracee.GoRoutineInfo, currUsedStackSize uint64) ([]callingFunction, []callingFunction, error) {
	status, _ := c.statusStore[goRoutineInfo.ID]
	callingFuncs := status.callingFunctions

	for i := len(callingFuncs) - 1; i >= 0; i-- {
		if callingFuncs[i].usedStackSize < currUsedStackSize {
			return callingFuncs[0 : i+1], callingFuncs[i+1:], nil

		} else if callingFuncs[i].usedStackSize == currUsedStackSize {
			currFunction, err := c.process.FindFunction(goRoutineInfo.CurrentPC)
			if err != nil {
				return nil, nil, err
			}

			if callingFuncs[i].Name == currFunction.Name {
				return callingFuncs[0 : i+1], callingFuncs[i+1:], nil
			}
		}

		unwindFunc := callingFuncs[i]
		if err := c.breakpoints.ClearConditional(unwindFunc.returnAddress, goRoutineInfo.ID); err != nil {
			return nil, nil, err
		}

		if unwindFunc.setCallInstBreakpoints {
			if err := c.clearCallInstBreakpoints(goRoutineInfo.ID, unwindFunc.StartAddr); err != nil {
				return nil, nil, err
			}
		}
	}
	return nil, callingFuncs, nil
}

func (c *Controller) addFunction(newFunc callingFunction, goRoutineID int64) error {
	status, _ := c.statusStore[goRoutineID]
	c.statusStore[goRoutineID] = goRoutineStatus{callingFunctions: append(status.callingFunctions, newFunc)}

	if err := c.breakpoints.SetConditional(newFunc.returnAddress, goRoutineID); err != nil {
		return err
	}

	if newFunc.setCallInstBreakpoints {
		return c.setCallInstBreakpoints(goRoutineID, newFunc.StartAddr)
	}
	return nil
}

func (c *Controller) handleTrapAtDeferredFuncCall(threadID int, goRoutineInfo tracee.GoRoutineInfo) error {
	if goRoutineInfo.Panicking && goRoutineInfo.PanicHandler != nil {
		_, _, err := c.unwindFunctions(goRoutineInfo, goRoutineInfo.PanicHandler.UsedStackSizeAtDefer)
		if err != nil {
			return err
		}
	}

	if err := c.handleTrapAtFunctionCall(threadID, goRoutineInfo.CurrentPC-1, goRoutineInfo); err != nil {
		return err
	}

	return c.breakpoints.ClearConditional(goRoutineInfo.CurrentPC-1, goRoutineInfo.ID)
}

func (c *Controller) handleTrapAfterFunctionReturn(threadID int, goRoutineInfo tracee.GoRoutineInfo) error {
	remainingFuncs, unwindedFuncs, err := c.unwindFunctions(goRoutineInfo, goRoutineInfo.UsedStackSize)
	if err != nil {
		return err
	}
	returnedFunc := unwindedFuncs[0].Function

	currStackDepth := len(remainingFuncs) + 1 // include returnedFunc for now
	prevStackFrame, err := c.prevStackFrame(goRoutineInfo, returnedFunc.StartAddr)
	if err != nil {
		return err
	}

	if currStackDepth <= c.traceLevel && prevStackFrame.Function.Name == "runtime.deferproc" {
		if err := c.setBreakpointToDeferredFunc(goRoutineInfo); err != nil {
			return err
		}
	}

	if currStackDepth <= c.traceLevel && c.printableFunc(returnedFunc) {
		if err := c.printFunctionOutput(goRoutineInfo.ID, prevStackFrame, currStackDepth); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) setBreakpointToDeferredFunc(goRoutineInfo tracee.GoRoutineInfo) error {
	nextAddr := goRoutineInfo.NextDeferFuncAddr
	if nextAddr == 0x0 /* no deferred func */ {
		return nil
	}

	if err := c.breakpoints.SetConditional(nextAddr, goRoutineInfo.ID); err != nil {
		return err
	}
	c.breakpointHints[nextAddr] = breakpointHintDeferredFunc
	return nil
}

// It must be called at the beginning of the function due to the StackFrameAt's constraint.
func (c *Controller) currentStackFrame(goRoutineInfo tracee.GoRoutineInfo) (*tracee.StackFrame, error) {
	return c.process.StackFrameAt(goRoutineInfo.CurrentStackAddr, goRoutineInfo.CurrentPC)
}

// It must be called at return address due to the StackFrameAt's constraint.
func (c *Controller) prevStackFrame(goRoutineInfo tracee.GoRoutineInfo, rip uint64) (*tracee.StackFrame, error) {
	return c.process.StackFrameAt(goRoutineInfo.CurrentStackAddr-8, rip)
}

func (c *Controller) printableFunc(f *tracee.Function) bool {
	const runtimePkgPrefix = "runtime."
	if strings.HasPrefix(f.Name, runtimePkgPrefix) {
		// it may be ok to print runtime unexported functions, but
		// these functions tend to be verbose and confusing.
		return f.IsExported()
	}

	return true
}

func (c *Controller) printFunctionInput(goRoutineID int64, stackFrame *tracee.StackFrame, depth int) error {
	var inputArgs []string
	for _, arg := range stackFrame.InputArguments {
		inputArgs = append(inputArgs, arg.ParseValue(c.parseLevel))
	}

	var outputArgs string
	if len(stackFrame.OutputArguments) > 0 {
		outputArgs = "..."
	}

	fmt.Fprintf(c.outputWriter, "%s\\ (#%02d) %s(%s) (%s)\n", strings.Repeat("|", depth-1), goRoutineID, stackFrame.Function.Name, strings.Join(inputArgs, ", "), outputArgs)

	return nil
}

func (c *Controller) printFunctionOutput(goRoutineID int64, stackFrame *tracee.StackFrame, depth int) error {
	var inputArgs []string
	for _, arg := range stackFrame.InputArguments {
		inputArgs = append(inputArgs, arg.ParseValue(c.parseLevel))
	}

	var outputArgs []string
	for _, arg := range stackFrame.OutputArguments {
		outputArgs = append(outputArgs, arg.ParseValue(c.parseLevel))
	}
	fmt.Fprintf(c.outputWriter, "%s/ (#%02d) %s(%s) (%s)\n", strings.Repeat("|", depth-1), goRoutineID, stackFrame.Function.Name, strings.Join(inputArgs, ", "), strings.Join(outputArgs, ", "))

	return nil
}

func (c *Controller) findCallInstAddresses(f *tracee.Function) ([]uint64, error) {
	// this cache is not only efficient, but required because there are no call insts if breakpoints are set.
	if cache, ok := c.callInstAddrCache[f.StartAddr]; ok {
		return cache, nil
	}

	insts, err := c.process.ReadInstructions(f)
	if err != nil {
		return nil, err
	}

	var pos int
	var addresses []uint64
	for _, inst := range insts {
		if inst.Op == x86asm.CALL || inst.Op == x86asm.LCALL {
			addresses = append(addresses, f.StartAddr+uint64(pos))
		}
		pos += inst.Len
	}

	c.callInstAddrCache[f.StartAddr] = addresses
	return addresses, nil
}

// Interrupt interrupts the main loop.
func (c *Controller) Interrupt() {
	c.interruptCh <- true
}
