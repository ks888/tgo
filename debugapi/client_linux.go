package debugapi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"

	"github.com/ks888/tgo/log"
	"golang.org/x/sys/unix"
)

// Client is the client proxy in order to execute the ptrace requests in the only one go routine.
// It is because the tracer thread must remain same, which is the limitation of ptrace.
type Client struct {
	reqCh  chan func()
	doneCh chan struct{}
	raw    *rawClient
}

// NewClient returns the new client proxy.
func NewClient() *Client {
	clientProxy := &Client{reqCh: make(chan func()), doneCh: make(chan struct{}), raw: newRawClient()}
	go func() {
		runtime.LockOSThread()

		// this go routine may leak, but it doesn't matter in typical use cases.
		for f := range clientProxy.reqCh {
			f()
			clientProxy.doneCh <- struct{}{}
		}
	}()
	return clientProxy
}

func (c *Client) LaunchProcess(name string, arg ...string) (err error) {
	c.reqCh <- func() { err = c.raw.LaunchProcess(name, arg...) }
	<-c.doneCh
	return
}

func (c *Client) AttachProcess(pid int) (err error) {
	c.reqCh <- func() { err = c.raw.AttachProcess(pid) }
	_ = <-c.doneCh
	return
}

func (c *Client) DetachProcess() (err error) {
	c.reqCh <- func() { err = c.raw.DetachProcess() }
	_ = <-c.doneCh
	return
}

func (c *Client) ReadMemory(addr uint64, out []byte) (err error) {
	c.reqCh <- func() { err = c.raw.ReadMemory(addr, out) }
	_ = <-c.doneCh
	return
}

func (c *Client) WriteMemory(addr uint64, data []byte) (err error) {
	c.reqCh <- func() { err = c.raw.WriteMemory(addr, data) }
	_ = <-c.doneCh
	return
}

func (c *Client) ReadRegisters(threadID int) (regs Registers, err error) {
	c.reqCh <- func() { regs, err = c.raw.ReadRegisters(threadID) }
	_ = <-c.doneCh
	return
}

func (c *Client) WriteRegisters(threadID int, regs Registers) (err error) {
	c.reqCh <- func() { err = c.raw.WriteRegisters(threadID, regs) }
	_ = <-c.doneCh
	return
}

func (c *Client) ReadTLS(threadID int, offset int32) (addr uint64, err error) {
	c.reqCh <- func() { addr, err = c.raw.ReadTLS(threadID, offset) }
	_ = <-c.doneCh
	return
}

func (c *Client) ContinueAndWait() (ev Event, err error) {
	c.reqCh <- func() { ev, err = c.raw.ContinueAndWait() }
	_ = <-c.doneCh
	return
}

func (c *Client) StepAndWait(threadID int) (ev Event, err error) {
	c.reqCh <- func() { ev, err = c.raw.StepAndWait(threadID) }
	_ = <-c.doneCh
	return
}

// rawClient is the debug api client which depends on OS API.
type rawClient struct {
	tracingProcessID int
	tracingThreadIDs []int
	trappedThreadIDs []int

	killOnDetach bool
}

// newRawClient returns the new debug api client which depends on linux ptrace.
func newRawClient() *rawClient {
	return &rawClient{}
}

// LaunchProcess launches the new prcoess with ptrace enabled.
func (c *rawClient) LaunchProcess(name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Ptrace: true,
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	c.killOnDetach = true

	// SIGTRAP signal is sent when execve is called.
	return c.waitAndInitialize(cmd.Process.Pid)
}

// AttachProcess attaches to the process.
func (c *rawClient) AttachProcess(pid int) error {
	// TODO: attach existing threads with the same pid
	if err := unix.PtraceAttach(pid); err != nil {
		return err
	}

	c.killOnDetach = false

	// SIGSTOP signal is sent when attached.
	return c.waitAndInitialize(pid)
}

func (c *rawClient) waitAndInitialize(pid int) error {
	var status unix.WaitStatus
	if _, err := unix.Wait4(pid, &status, 0, nil); err != nil {
		return err
	}

	if !status.Stopped() {
		return fmt.Errorf("process is not stopped: %#v", status)
	} else if status.StopSignal() != syscall.SIGTRAP && status.StopSignal() != syscall.SIGSTOP {
		return fmt.Errorf("unexpected signal: %s", status.StopSignal())
	}

	unix.PtraceSetOptions(pid, unix.PTRACE_O_TRACECLONE)

	c.tracingProcessID = pid
	c.tracingThreadIDs = append(c.tracingThreadIDs, pid)
	c.trappedThreadIDs = append(c.trappedThreadIDs, pid)

	return nil
}

// DetachProcess detaches from the process.
func (c *rawClient) DetachProcess() error {
	// detach the processes even when we will kill them soon, because
	// next wait call may receive the terminated event of these processes.
	for _, pid := range c.tracingThreadIDs {
		if err := unix.PtraceDetach(pid); err != nil {
			// the process may have exited already
			log.Debugf("failed to detach %d: %v", pid, err)
		}
	}

	if c.killOnDetach {
		return c.killProcess()
	}

	return nil
}

func (c *rawClient) killProcess() error {
	// it may be exited already
	proc, _ := os.FindProcess(c.tracingProcessID)
	_ = proc.Kill()

	// We can't simply call proc.Wait, since it will hang when the thread leader exits while there are still subthreads.
	// By calling wait4 like below, it reaps the subthreads first and then reaps the thread leader.
	var status unix.WaitStatus
	for {
		if wpid, err := unix.Wait4(-1, &status, 0, nil); err != nil || wpid == c.tracingProcessID {
			return err
		}
	}
}

// ReadMemory reads the specified memory region in the prcoess.
func (c *rawClient) ReadMemory(addr uint64, out []byte) error {
	if len(c.trappedThreadIDs) == 0 {
		return errors.New("failed to read memory: currently no trapped threads")
	}

	count, err := unix.PtracePeekData(c.trappedThreadIDs[0], uintptr(addr), out)
	if err != nil {
		return err
	} else if count != len(out) {
		return fmt.Errorf("the number of data read is invalid: expect: %d, actual %d", len(out), count)
	}
	return nil
}

// WriteMemory write the data to the specified memory region in the prcoess.
func (c *rawClient) WriteMemory(addr uint64, data []byte) error {
	if len(c.trappedThreadIDs) == 0 {
		return errors.New("failed to write memory: currently no trapped threads")
	}

	count, err := unix.PtracePokeData(c.trappedThreadIDs[0], uintptr(addr), data)
	if err != nil {
		return err
	} else if count != len(data) {
		return fmt.Errorf("the number of data written is invalid: expect: %d, actual %d", len(data), count)
	}
	return nil
}

// ReadRegisters reads the registers of the prcoess.
func (c *rawClient) ReadRegisters(threadID int) (regs Registers, err error) {
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
func (c *rawClient) WriteRegisters(threadID int, regs Registers) error {
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
func (c *rawClient) ReadTLS(threadID int, offset int32) (uint64, error) {
	var rawRegs unix.PtraceRegs
	if err := unix.PtraceGetRegs(threadID, &rawRegs); err != nil {
		return 0, err
	}

	buff := make([]byte, 8)
	if err := c.ReadMemory(rawRegs.Fs_base+uint64(offset), buff); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(buff), nil
}

// ContinueAndWait resumes the list of processes and waits until an event happens.
func (c *rawClient) ContinueAndWait() (Event, error) {
	return c.continueAndWait(0)
}

func (c *rawClient) continueAndWait(sig int) (Event, error) {
	for _, threadID := range c.trappedThreadIDs {
		if err := unix.PtraceCont(threadID, sig); err != nil {
			return Event{}, err
		}
	}
	c.trappedThreadIDs = nil

	var status unix.WaitStatus
	waitedThreadID, err := unix.Wait4(-1 /* any tracing thread */, &status, 0, nil)
	if err != nil {
		return Event{}, err
	}

	return c.handleWaitStatus(status, waitedThreadID)
}

// StepAndWait executes the single instruction of the specified process and waits until an event happens.
// Note that an event happens to any children of the current process is reported.
func (c *rawClient) StepAndWait(threadID int) (Event, error) {
	if err := unix.PtraceSingleStep(threadID); err != nil {
		return Event{}, err
	}

	for i, candidate := range c.trappedThreadIDs {
		if candidate == threadID {
			c.trappedThreadIDs = append(c.trappedThreadIDs[0:i], c.trappedThreadIDs[i+1:]...)
		}
	}

	var status unix.WaitStatus
	waitedThreadID, err := unix.Wait4(threadID, &status, unix.WNOTHREAD, nil)
	if err != nil {
		return Event{}, err
	}

	return c.handleWaitStatus(status, waitedThreadID)
}

func (c *rawClient) handleWaitStatus(status unix.WaitStatus, threadID int) (event Event, err error) {
	if status.Stopped() {
		c.trappedThreadIDs = append(c.trappedThreadIDs, threadID)

		if status.StopSignal() == unix.SIGTRAP {
			if status.TrapCause() == unix.PTRACE_EVENT_CLONE {
				_, err := c.continueClone(threadID)
				if err != nil {
					return Event{}, err
				}
				return c.continueAndWait(0)
			}

			event = Event{Type: EventTypeTrapped, Data: []int{threadID}}
		} else {
			return c.continueAndWait(int(status.StopSignal()))
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

func (c *rawClient) continueClone(parentThreadID int) (int, error) {
	clonedThreadID, err := unix.PtraceGetEventMsg(parentThreadID)
	if err != nil {
		return 0, err
	}
	c.tracingThreadIDs = append(c.tracingThreadIDs, int(clonedThreadID))

	// Cloned process may not exist yet.
	if _, err := unix.Wait4(int(clonedThreadID), nil, 0, nil); err != nil {
		return 0, err
	}
	err = unix.PtraceCont(int(clonedThreadID), 0)
	return int(clonedThreadID), err
}
