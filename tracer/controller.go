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
	// clearedBreakpoints is the address the break point should be set, but temporarily cleared by the go routine for single stepping.
	// Usually the function doesn't change after the single stepping and so this address is not necessary,
	// but the function changes when the function 'CALL's at the beginning of the function.
	clearedBreakpoint uint64
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
	stackFrame, err := c.process.CurrentStackFrame()
	if err != nil {
		return debugapi.Event{}, err
	}
	funcAddr := stackFrame.Function.Value

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
		// TODO: enable the condition after supporting the return case.
		// if goRoutineInfo.UsedStackSize != status.usedStackSize {
		// If the size is same as before, it's likely we are still in the same stack frame (typical for the stack growth case).
		if err := c.printFunction(goRoutineID, stackFrame); err != nil {
			return debugapi.Event{}, err
		}
		// }

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
			statusType:        goRoutineSingleStepping,
			usedStackSize:     goRoutineInfo.UsedStackSize,
			clearedBreakpoint: funcAddr,
		}
		return c.process.StepAndWait()

	case goRoutineSingleStepping:
		if err := c.process.SetBreakpoint(status.clearedBreakpoint); err != nil {
			return debugapi.Event{}, err
		}

		status.statusType = goRoutineRunning
		status.clearedBreakpoint = 0
		c.statusStore[goRoutineID] = status
		return c.process.ContinueAndWait()

	default:
		return debugapi.Event{}, fmt.Errorf("unknown status: %v", status.statusType)
	}
}

func (c *Controller) printFunction(goRoutineID int, stackFrame *tracee.StackFrame) error {
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
	fmt.Printf("#%02d %s(%s)\n", goRoutineID, stackFrame.Function.Name, strings.Join(args, ", "))

	return nil
}

// Interrupt has the main loop exit.
func (c *Controller) Interrupt() {
	c.interrupted = true
}
