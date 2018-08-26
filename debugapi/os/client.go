package os

import (
	"fmt"
	"os/exec"
	"syscall"

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
func (c Client) LaunchProcess(name string, arg ...string) (pid int, err error) {
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
