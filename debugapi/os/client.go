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
		return 0, fmt.Errorf("unexpected process status: %v, %d", status.Stopped(), status.StopSignal())
	}

	unix.PtraceSetOptions(cmd.Process.Pid, unix.PTRACE_O_TRACECLONE)

	return cmd.Process.Pid, nil
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

// ContinueAndWait restart the process and waits until an event happens.
func (c Client) ContinueAndWait(pid int) (wpid int, event debugapi.Event, err error) {
	var sig int
	for {
		if err = unix.PtraceCont(pid, sig); err != nil {
			return
		}
		sig = 0

		var status unix.WaitStatus
		wpid, err = unix.Wait4(pid, &status, 0, nil)
		if err != nil {
			return
		}

		if status.Stopped() {
			if status.StopSignal() == unix.SIGTRAP {
				if status.TrapCause() == unix.PTRACE_EVENT_CLONE {
					var clonedPid uint
					clonedPid, err = unix.PtraceGetEventMsg(pid)
					if err != nil {
						return
					}

					// Cloned process may not exist yet.
					if _, err = unix.Wait4(int(clonedPid), &status, 0, nil); err != nil {
						return
					}
					if err = unix.PtraceCont(int(clonedPid), 0); err != nil {
						return
					}

					event = debugapi.Event{Type: debugapi.EventTypeCreated, Data: int(clonedPid)}
				} else {
					event = debugapi.Event{Type: debugapi.EventTypeTrapped}
				}
				return
			}

			sig = int(status.StopSignal())
		} else if status.Exited() {
			event = debugapi.Event{Type: debugapi.EventTypeExited, Data: status.ExitStatus()}
			return
		} else if status.CoreDump() {
			event = debugapi.Event{Type: debugapi.EventTypeCoreDump}
			return
		} else if status.Signaled() {
			event = debugapi.Event{Type: debugapi.EventTypeTerminated, Data: int(status.Signal())}
			return
		}
	}
}
