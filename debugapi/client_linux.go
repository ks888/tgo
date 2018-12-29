package debugapi

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// TODO: wrap client so that the ptrace action is execed from always same thread.

// Client is the debug api client which depends on OS API.
type Client struct {
	trappedThreadIDs []int
	tracingThreadIDs []int
}

// NewClient returns the new debug api client which depends on OS API.
func NewClient() *Client {
	return &Client{}
}

// LaunchProcess launches the new prcoess with ptrace enabled.
func (c *Client) LaunchProcess(name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Ptrace: true,
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	return c.waitAndInitialize(cmd.Process.Pid)
}

// AttachProcess attaches to the process.
func (c *Client) AttachProcess(pid int) error {
	// TODO: attach existing threads with the same pid
	if err := unix.PtraceAttach(pid); err != nil {
		return err
	}

	return c.waitAndInitialize(pid)
}

func (c *Client) waitAndInitialize(pid int) error {
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

	c.trappedThreadIDs = append(c.trappedThreadIDs, pid)
	c.tracingThreadIDs = append(c.tracingThreadIDs, pid)

	return nil
}

// DetachProcess detaches from the process.
func (c *Client) DetachProcess() error {
	// TODO: kill the debugee process if it is launched by us.
	for _, pid := range c.tracingThreadIDs {
		if err := unix.PtraceDetach(pid); err != nil {
			return err
		}
	}
	return nil
}

// ReadMemory reads the specified memory region in the prcoess.
func (c *Client) ReadMemory(addr uintptr, out []byte) error {
	if len(c.trappedThreadIDs) == 0 {
		return errors.New("failed to read memory: currently no trapped threads")
	}

	count, err := unix.PtracePeekData(c.trappedThreadIDs[0], addr, out)
	if count != len(out) {
		return fmt.Errorf("the number of data read is invalid: %d", count)
	}
	return err
}

// WriteMemory write the data to the specified memory region in the prcoess.
func (c *Client) WriteMemory(addr uintptr, data []byte) error {
	if len(c.trappedThreadIDs) == 0 {
		return errors.New("failed to read memory: currently no trapped threads")
	}

	count, err := unix.PtracePokeData(c.trappedThreadIDs[0], addr, data)
	if count != len(data) {
		return fmt.Errorf("the number of data written is invalid: %d", count)
	}
	return err
}

// ReadRegisters reads the registers of the prcoess.
func (c *Client) ReadRegisters(threadID int) (regs Registers, err error) {
	var rawRegs unix.PtraceRegs
	if err = unix.PtraceGetRegs(threadID, &rawRegs); err != nil {
		return regs, err
	}

	regs.Rip = rawRegs.Rip
	regs.Rsp = rawRegs.Rsp
	regs.Rcx = rawRegs.Rcx
	return regs, nil
}

// WriteRegisters change the registers of the prcoess.
func (c *Client) WriteRegisters(threadID int, regs *Registers) error {
	var rawRegs unix.PtraceRegs
	if err := unix.PtraceGetRegs(threadID, &rawRegs); err != nil {
		return err
	}

	rawRegs.Rip = regs.Rip
	rawRegs.Rsp = regs.Rsp
	rawRegs.Rcx = regs.Rcx
	return unix.PtraceSetRegs(threadID, &rawRegs)
}

// ReadTLS reads the offset from the beginning of the TLS block.
func (c *Client) ReadTLS(threadID int, offset uint32) (uint64, error) {
	// TODO: impl
	return 0, nil
}

// ContinueAndWait resumes the list of processes and waits until an event happens.
func (c *Client) ContinueAndWait() (Event, error) {
	return c.continueAndWait(c.trappedThreadIDs, 0)
}

func (c *Client) continueAndWait(threadIDsToContinue []int, sig int) (Event, error) {
	for _, threadID := range threadIDsToContinue {
		if err := unix.PtraceCont(threadID, sig); err != nil {
			return Event{}, err
		}
	}

	return c.wait(-1)
}

// StepAndWait executes the single instruction of the specified process and waits until an event happens.
// Note that an event happens to any children of the current process is reported.
func (c *Client) StepAndWait(threadID int) (Event, error) {
	if err := unix.PtraceSingleStep(threadID); err != nil {
		return Event{}, err
	}

	return c.wait(-1)
}

func (c *Client) wait(threadID int) (Event, error) {
	var status unix.WaitStatus
	waitedThreadID, err := unix.Wait4(threadID, &status, 0, nil)
	if err != nil {
		return Event{}, err
	}

	var event Event
	if status.Stopped() {
		if status.StopSignal() == unix.SIGTRAP {
			if status.TrapCause() == unix.PTRACE_EVENT_CLONE {
				_, err := c.continueClone(waitedThreadID)
				if err != nil {
					return Event{}, err
				}

				return c.continueAndWait([]int{waitedThreadID}, 0)
			}

			event = Event{Type: EventTypeTrapped, Data: []int{waitedThreadID}}
		} else {
			return c.continueAndWait([]int{waitedThreadID}, int(status.StopSignal()))
		}
	} else if status.Exited() {
		event = Event{Type: EventTypeExited, Data: status.ExitStatus()}
	} else if status.CoreDump() {
		event = Event{Type: EventTypeCoreDump}
	} else if status.Signaled() {
		event = Event{Type: EventTypeTerminated, Data: int(status.Signal())}
	}
	return event, nil
}

func (c *Client) continueClone(parentThreadID int) (int, error) {
	clonedThreadID, err := unix.PtraceGetEventMsg(parentThreadID)
	if err != nil {
		return 0, err
	}

	// Cloned process may not exist yet.
	if _, err := unix.Wait4(int(clonedThreadID), nil, 0, nil); err != nil {
		return 0, err
	}
	err = unix.PtraceCont(int(clonedThreadID), 0)
	return int(clonedThreadID), err
}
