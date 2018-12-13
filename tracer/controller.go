package tracer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ks888/tgo/debugapi"
	"github.com/ks888/tgo/log"
	"github.com/ks888/tgo/tracee"
)

const chanBufferSize = 64

// ErrInterrupted indicates the tracer is interrupted due to the Interrupt() call.
var ErrInterrupted = errors.New("interrupted")

// Controller controls the associated tracee process.
type Controller struct {
	process     *tracee.Process
	statusStore map[int]goRoutineStatus

	tracingPoints tracingPoints
	traceLevel    int
	parseLevel    int

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

func (status goRoutineStatus) lastFunctionAddr() uint64 {
	if len(status.callingFunctions) > 0 {
		return status.callingFunctions[len(status.callingFunctions)-1].Value
	}
	return 0
}

type callingFunction struct {
	*tracee.Function
	returnAddress uint64
	usedStackSize uint64
}

// NewController returns the new controller.
func NewController() *Controller {
	return &Controller{
		outputWriter:           os.Stdout,
		interruptCh:            make(chan bool, chanBufferSize),
		pendingStartTracePoint: make(chan uint64, chanBufferSize),
		pendingEndTracePoint:   make(chan uint64, chanBufferSize),
	}
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

// AddStartTracePoint adds the starting point of the tracing. The go routines which executed one of these addresses start to be traced.
func (c *Controller) AddStartTracePoint(startAddr uint64) error {
	select {
	case c.pendingStartTracePoint <- startAddr:
	default:
		// maybe buffer full
		return errors.New("failed to add start trace point")
	}
	return nil
}

// AddEndTracePoint adds the ending point of the tracing. The tracing is disabled when any go routine executes any of these addresses.
func (c *Controller) AddEndTracePoint(endAddr uint64) error {
	select {
	case c.pendingEndTracePoint <- endAddr:
	default:
		// maybe buffer full
		return errors.New("failed to add end trace point")
	}
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
	return nil, fmt.Errorf("failed to find function %s", functionName)
}

// canSetBreakpoint returns true if it's safe to set the breakpoint at the given function.
// Most unexported runtime functions are not supported, because these functions make the tracer unstable (especially in the case the system stack is used).
func (c *Controller) canSetBreakpoint(function *tracee.Function) bool {
	const runtimePrefix = "runtime."
	if strings.HasPrefix(function.Name, runtimePrefix) {
		return function.IsExported() || function.Name == "runtime.gopanic" // need to trap gopanic function in order to calculate a stack depth correctly.
	}

	return true
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
	defer func() { _ = c.process.Detach() }() // the connection status is unknown at this point

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
		if err := c.process.Detach(); err != nil {
			return debugapi.Event{}, err
		}
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

			if err := c.process.SetBreakpoint(startAddr); err != nil {
				return err
			}
			c.tracingPoints.startAddressList = append(c.tracingPoints.startAddressList, startAddr)

		case endAddr := <-c.pendingEndTracePoint:
			if c.tracingPoints.IsEndAddress(endAddr) {
				continue // set already
			}

			if err := c.process.SetBreakpoint(endAddr); err != nil {
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
			return debugapi.Event{}, err
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
	if !c.process.HitBreakpoint(breakpointAddr, goRoutineInfo.ID) {
		return c.handleTrapAtUnrelatedBreakpoint(threadID, breakpointAddr)
	}

	if c.tracingPoints.IsStartAddress(breakpointAddr) {
		return c.enterTracepoint(threadID, goRoutineInfo)

	} else if c.tracingPoints.IsEndAddress(breakpointAddr) {
		return c.exitTracepoint(threadID, goRoutineInfo)

	} else if !c.traced(goRoutineInfo.ID, goRoutineInfo.Ancestors) {
		return c.handleTrapAtUnrelatedBreakpoint(threadID, breakpointAddr)
	}

	status, _ := c.statusStore[int(goRoutineInfo.ID)]
	if goRoutineInfo.UsedStackSize == status.usedStackSize() && breakpointAddr == status.lastFunctionAddr() {
		// it's likely we are in the same stack frame as before (typical in the stack growth case).
		return c.handleTrapAtUnrelatedBreakpoint(threadID, breakpointAddr)
	} else if c.isFunctionCall(breakpointAddr) {
		return c.handleTrapAtFunctionCall(threadID, goRoutineInfo)
	}

	return c.handleTrapAtFunctionReturn(threadID, goRoutineInfo)
}

func (c *Controller) enterTracepoint(threadID int, goRoutineInfo tracee.GoRoutineInfo) error {
	if c.tracingPoints.Empty() {
		if err := c.setBreakpointsExceptTracingPoint(); err != nil {
			return err
		}
	}

	c.tracingPoints.Enter(goRoutineInfo.ID)
	breakpointAddr := goRoutineInfo.CurrentPC - 1
	return c.handleTrapAtUnrelatedBreakpoint(threadID, breakpointAddr)
}

func (c *Controller) exitTracepoint(threadID int, goRoutineInfo tracee.GoRoutineInfo) error {
	if !c.tracingPoints.Empty() {
		if err := c.clearBreakpointsExceptTracingPoint(); err != nil {
			return err
		}
	}

	c.tracingPoints.Exit()
	breakpointAddr := goRoutineInfo.CurrentPC - 1
	return c.handleTrapAtUnrelatedBreakpoint(threadID, breakpointAddr)
}

func (c *Controller) handleTrappedSystemRoutine(threadID int) error {
	threadInfo, err := c.process.CurrentThreadInfo(threadID)
	if err != nil {
		return err
	}

	breakpointAddr := threadInfo.CurrentPC - 1
	return c.process.SingleStep(threadID, breakpointAddr)
}

func (c *Controller) traced(goRoutineID int64, parentIDs []int64) bool {
	for _, parentID := range append([]int64{goRoutineID}, parentIDs...) {
		if c.tracingPoints.Inside(parentID) {
			return true
		}
	}
	return false
}

func (c *Controller) isFunctionCall(breakpointAddr uint64) bool {
	function, err := c.process.Binary.FindFunction(breakpointAddr)
	if err != nil {
		log.Debugf("failed to find function (addr: %x): %v", breakpointAddr, err)
		return false
	}

	return function.Value == breakpointAddr
}

func (c *Controller) handleTrapAtUnrelatedBreakpoint(threadID int, breakpointAddr uint64) error {
	return c.process.SingleStep(threadID, breakpointAddr)
}

func (c *Controller) handleTrapAtFunctionCall(threadID int, goRoutineInfo tracee.GoRoutineInfo) error {
	status, _ := c.statusStore[int(goRoutineInfo.ID)]
	stackFrame, err := c.currentStackFrame(goRoutineInfo)
	if err != nil {
		return err
	}

	// unwinded here in some cases:
	// * just recovered from panic.
	// * the last function used 'JMP' to call the next function and didn't change the SP. e.g. runtime.deferreturn
	remainingFuncs, _, err := c.unwindFunctions(status.callingFunctions, goRoutineInfo)
	if err != nil {
		return err
	}

	callingFunc := callingFunction{
		Function:      stackFrame.Function,
		returnAddress: stackFrame.ReturnAddress,
		usedStackSize: goRoutineInfo.UsedStackSize,
	}
	remainingFuncs, err = c.appendFunction(remainingFuncs, callingFunc, goRoutineInfo.ID)
	if err != nil {
		return err
	}

	currStackDepth := len(remainingFuncs)
	if goRoutineInfo.Panicking && goRoutineInfo.PanicHandler != nil {
		currStackDepth -= c.countSkippedFuncs(status.callingFunctions, goRoutineInfo.PanicHandler.UsedStackSizeAtDefer)
	}

	if c.canPrint(goRoutineInfo.ID, goRoutineInfo.Ancestors, currStackDepth) {
		if err := c.printFunctionInput(goRoutineInfo.ID, stackFrame, currStackDepth); err != nil {
			return err
		}
	}

	if err := c.process.SingleStep(threadID, stackFrame.Function.Value); err != nil {
		return err
	}

	c.statusStore[int(goRoutineInfo.ID)] = goRoutineStatus{callingFunctions: remainingFuncs}
	return nil
}

func (c *Controller) countSkippedFuncs(callingFuncs []callingFunction, usedStackSize uint64) int {
	panicFuncIndex := c.findPanicFunction(callingFuncs)
	if panicFuncIndex == -1 {
		return 0
	}

	for i := panicFuncIndex; i >= 0; i-- {
		if callingFuncs[i].usedStackSize < usedStackSize {
			return panicFuncIndex - i
		}
	}
	return panicFuncIndex + 1
}

func (c *Controller) findPanicFunction(callingFuncs []callingFunction) int {
	for i, callingFunc := range callingFuncs {
		if callingFunc.Name == "runtime.gopanic" {
			return i
		}
	}
	return -1
}

func (c *Controller) unwindFunctions(callingFuncs []callingFunction, goRoutineInfo tracee.GoRoutineInfo) ([]callingFunction, []callingFunction, error) {
	for i := len(callingFuncs) - 1; i >= 0; i-- {
		if callingFuncs[i].usedStackSize < goRoutineInfo.UsedStackSize {
			return callingFuncs[0 : i+1], callingFuncs[i+1:], nil

		} else if callingFuncs[i].usedStackSize == goRoutineInfo.UsedStackSize {
			breakpointAddr := goRoutineInfo.CurrentPC - 1
			currFunction, err := c.process.Binary.FindFunction(breakpointAddr)
			if err != nil {
				return nil, nil, err
			}

			if callingFuncs[i].Name == currFunction.Name {
				return callingFuncs[0 : i+1], callingFuncs[i+1:], nil
			}
		}

		retAddr := callingFuncs[i].returnAddress
		if err := c.process.ClearConditionalBreakpoint(retAddr, goRoutineInfo.ID); err != nil {
			return nil, nil, err
		}
	}
	return nil, callingFuncs, nil
}

func (c *Controller) appendFunction(callingFuncs []callingFunction, newFunc callingFunction, goRoutineID int64) ([]callingFunction, error) {
	if err := c.process.SetConditionalBreakpoint(newFunc.returnAddress, goRoutineID); err != nil {
		return nil, err
	}
	return append(callingFuncs, newFunc), nil
}

func (c *Controller) setBreakpointsExceptTracingPoint() error {
	return c.alterBreakpointsExceptTracingPoint(true)
}

func (c *Controller) clearBreakpointsExceptTracingPoint() error {
	return c.alterBreakpointsExceptTracingPoint(false)
}

func (c *Controller) alterBreakpointsExceptTracingPoint(enable bool) error {
	functions, err := c.process.Binary.ListFunctions()
	if err != nil {
		return err
	}
	for _, function := range functions {
		if !c.canSetBreakpoint(function) || c.tracingPoints.IsStartAddress(function.Value) || c.tracingPoints.IsEndAddress(function.Value) {
			continue
		}

		if enable {
			err = c.process.SetBreakpoint(function.Value)
		} else {
			err = c.process.ClearBreakpoint(function.Value)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) canPrint(goRoutineID int64, parentIDs []int64, currStackDepth int) bool {
	for _, parentID := range append([]int64{goRoutineID}, parentIDs...) {
		if c.tracingPoints.Inside(parentID) {
			return currStackDepth <= c.traceLevel
		}
	}
	return false
}

func (c *Controller) handleTrapAtFunctionReturn(threadID int, goRoutineInfo tracee.GoRoutineInfo) error {
	status, _ := c.statusStore[int(goRoutineInfo.ID)]

	remainingFuncs, unwindedFuncs, err := c.unwindFunctions(status.callingFunctions, goRoutineInfo)
	if err != nil {
		return err
	}
	returnedFunc := unwindedFuncs[0].Function

	currStackDepth := len(remainingFuncs) + 1 // include returnedFunc for now
	if goRoutineInfo.Panicking && goRoutineInfo.PanicHandler != nil {
		currStackDepth -= c.countSkippedFuncs(status.callingFunctions, goRoutineInfo.PanicHandler.UsedStackSizeAtDefer)
	}

	if c.canPrint(goRoutineInfo.ID, goRoutineInfo.Ancestors, currStackDepth) {
		prevStackFrame, err := c.prevStackFrame(goRoutineInfo, returnedFunc.Value)
		if err != nil {
			return err
		}
		if err := c.printFunctionOutput(goRoutineInfo.ID, prevStackFrame, currStackDepth); err != nil {
			return err
		}
	}

	if err := c.process.SingleStep(threadID, goRoutineInfo.CurrentPC-1); err != nil {
		return err
	}

	c.statusStore[int(goRoutineInfo.ID)] = goRoutineStatus{callingFunctions: remainingFuncs}
	return nil
}

// It must be called at the beginning of the function due to the StackFrameAt's constraint.
func (c *Controller) currentStackFrame(goRoutineInfo tracee.GoRoutineInfo) (*tracee.StackFrame, error) {
	return c.process.StackFrameAt(goRoutineInfo.CurrentStackAddr, goRoutineInfo.CurrentPC-1)
}

// It must be called at return address due to the StackFrameAt's constraint.
func (c *Controller) prevStackFrame(goRoutineInfo tracee.GoRoutineInfo, rip uint64) (*tracee.StackFrame, error) {
	return c.process.StackFrameAt(goRoutineInfo.CurrentStackAddr-8, rip)
}

func (c *Controller) printFunctionInput(goRoutineID int64, stackFrame *tracee.StackFrame, depth int) error {
	var args []string
	for _, arg := range stackFrame.InputArguments {
		args = append(args, arg.ParseValue(c.parseLevel))
	}

	fmt.Fprintf(c.outputWriter, "%s\\ (#%02d) %s(%s)\n", strings.Repeat("|", depth-1), goRoutineID, stackFrame.Function.Name, strings.Join(args, ", "))

	return nil
}

func (c *Controller) printFunctionOutput(goRoutineID int64, stackFrame *tracee.StackFrame, depth int) error {
	var args []string
	for _, arg := range stackFrame.OutputArguments {
		args = append(args, arg.ParseValue(c.parseLevel))
	}
	fmt.Fprintf(c.outputWriter, "%s/ (#%02d) %s() (%s)\n", strings.Repeat("|", depth-1), goRoutineID, stackFrame.Function.Name, strings.Join(args, ", "))

	return nil
}

// Interrupt interrupts the main loop.
func (c *Controller) Interrupt() {
	c.interruptCh <- true
}

type tracingPoints struct {
	startAddressList []uint64
	endAddressList   []uint64
	goRoutinesInside []int64
}

// IsStartAddress returns true if the addr is same as the start address.
func (p *tracingPoints) IsStartAddress(addr uint64) bool {
	for _, startAddr := range p.startAddressList {
		if startAddr == addr {
			return true
		}
	}
	return false
}

// IsEndAddress returns true if the addr is same as the end address.
func (p *tracingPoints) IsEndAddress(addr uint64) bool {
	for _, endAddr := range p.endAddressList {
		if endAddr == addr {
			return true
		}
	}
	return false
}

// Empty returns true if no go routines are inside the tracing point
func (p *tracingPoints) Empty() bool {
	return len(p.goRoutinesInside) == 0
}

// Enter updates the list of the go routines which are inside the tracing point.
// It does nothing if the go routine has already entered.
func (p *tracingPoints) Enter(goRoutineID int64) {
	for _, existingGoRoutine := range p.goRoutinesInside {
		if existingGoRoutine == goRoutineID {
			return
		}
	}

	log.Debugf("Start tracing")
	p.goRoutinesInside = append(p.goRoutinesInside, goRoutineID)
	return
}

// Exit clears the inside go routines list.
func (p *tracingPoints) Exit() {
	log.Debugf("End tracing")
	p.goRoutinesInside = nil
}

// Inside returns true if the go routine is inside the tracing point.
func (p *tracingPoints) Inside(goRoutineID int64) bool {
	for _, existingGoRoutine := range p.goRoutinesInside {
		if existingGoRoutine == goRoutineID {
			return true
		}
	}
	return false
}
