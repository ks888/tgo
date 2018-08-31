package os

import (
	"fmt"
	"os/exec"
	"syscall"

	"github.com/ks888/tgo/debugapi"
	"golang.org/x/sys/unix"
)

// TODO: filename client.go -> client_linux.go
// TODO: change impl at build time using build tag.
// TODO: wrap client so that the ptrace action is execed from always same thread.

// Client is the debug api client which depends on OS API.
type Client struct {
}

// NewClient returns the new debug api client which depends on OS API.
func NewClient() Client {
	return Client{}
}

// LaunchProcess launches the new prcoess with ptrace enabled.
func (c Client) LaunchProcess(name string, arg ...string) (int, error) {
	cmd := exec.Command(name, arg...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Ptrace: true,
	}

	if err := cmd.Start(); err != nil {
		return 0, err
	}

	var status unix.WaitStatus
	if _, err := unix.Wait4(cmd.Process.Pid, &status, 0, nil); err != nil {
		return 0, err
	}

	if !status.Stopped() || status.StopSignal() != syscall.SIGTRAP {
		return 0, fmt.Errorf("process is not stopped or the signal (%s) is invalid", status.StopSignal())
	}

	unix.PtraceSetOptions(cmd.Process.Pid, unix.PTRACE_O_TRACECLONE)

	return cmd.Process.Pid, nil
}

// AttachProcess attaches to the process.
func (c Client) AttachProcess(pid int) error {
	if err := unix.PtraceAttach(pid); err != nil {
		return err
	}

	var status unix.WaitStatus
	if _, err := unix.Wait4(pid, &status, 0, nil); err != nil {
		return err
	}

	// we may repeatedly wait the pid until SIGSTOP is sent, but it makes the logic more complicated.
	// So chose the simlple strategy.
	if !status.Stopped() || status.StopSignal() != syscall.SIGSTOP {
		return fmt.Errorf("process is not stopped or the signal (%s) is invalid", status.StopSignal())
	}

	unix.PtraceSetOptions(pid, unix.PTRACE_O_TRACECLONE)

	return nil
}

// DetachProcess detaches from the process.
func (c Client) DetachProcess(pid int) error {
	return unix.PtraceDetach(pid)
}

// ReadMemory reads the specified memory region in the prcoess.
func (c Client) ReadMemory(pid int, addr uintptr, out []byte) error {
	count, err := unix.PtracePeekData(pid, addr, out)
	if count != len(out) {
		return fmt.Errorf("the number of data read is invalid: %d", count)
	}
	return err
}

// WriteMemory write the data to the specified memory region in the prcoess.
func (c Client) WriteMemory(pid int, addr uintptr, out []byte) error {
	count, err := unix.PtracePokeData(pid, addr, out)
	if count != len(out) {
		return fmt.Errorf("the number of data written is invalid: %d", count)
	}
	return err
}

// ReadRegisters reads the registers of the prcoess.
func (c Client) ReadRegisters(pid int, regs *unix.PtraceRegs) error {
	return unix.PtraceGetRegs(pid, regs)
}

// WriteRegisters change the registers of the prcoess.
func (c Client) WriteRegisters(pid int, regs *unix.PtraceRegs) error {
	return unix.PtraceSetRegs(pid, regs)
}

// ContinueAndWait restart the list of processes and waits until an event happens.
// Note that an event happens to any children of the current process is reported.
func (c Client) ContinueAndWait(pidsToContinue ...int) (waitedPID int, event debugapi.Event, err error) {
	return c.continueAndWait(pidsToContinue, 0)
}

func (c Client) continueAndWait(pidsToContinue []int, sig int) (int, debugapi.Event, error) {
	for _, pid := range pidsToContinue {
		if err := unix.PtraceCont(pid, sig); err != nil {
			return 0, debugapi.Event{}, err
		}
	}

	return c.wait(-1)
}

// StepAndWait executes the single instruction of the specified process and waits until an event happens.
// Note that an event happens to any children of the current process is reported.
func (c Client) StepAndWait(pid int) (waitedPID int, event debugapi.Event, err error) {
	if err := unix.PtraceSingleStep(pid); err != nil {
		return 0, debugapi.Event{}, err
	}

	return c.wait(-1)
}

func (c Client) wait(pid int) (int, debugapi.Event, error) {
	var status unix.WaitStatus
	waitedPid, err := unix.Wait4(pid, &status, 0, nil)
	if err != nil {
		return 0, debugapi.Event{}, err
	}

	var event debugapi.Event
	if status.Stopped() {
		if status.StopSignal() == unix.SIGTRAP {
			if status.TrapCause() == unix.PTRACE_EVENT_CLONE {
				clonedPid, err := c.continueClone(waitedPid)
				if err != nil {
					return 0, debugapi.Event{}, err
				}

				event = debugapi.Event{Type: debugapi.EventTypeCreated, Data: clonedPid}
			} else {
				event = debugapi.Event{Type: debugapi.EventTypeTrapped}
			}
		} else {
			return c.continueAndWait([]int{waitedPid}, int(status.StopSignal()))
		}
	} else if status.Exited() {
		event = debugapi.Event{Type: debugapi.EventTypeExited, Data: status.ExitStatus()}
	} else if status.CoreDump() {
		event = debugapi.Event{Type: debugapi.EventTypeCoreDump}
	} else if status.Signaled() {
		event = debugapi.Event{Type: debugapi.EventTypeTerminated, Data: int(status.Signal())}
	}
	return waitedPid, event, nil
}

func (c Client) continueClone(parentPID int) (int, error) {
	clonedPid, err := unix.PtraceGetEventMsg(parentPID)
	if err != nil {
		return 0, err
	}

	// Cloned process may not exist yet.
	if _, err := unix.Wait4(int(clonedPid), nil, 0, nil); err != nil {
		return 0, err
	}
	err = unix.PtraceCont(int(clonedPid), 0)
	return int(clonedPid), err
}
