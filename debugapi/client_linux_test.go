package debugapi

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"syscall"
	"testing"

	"github.com/ks888/tgo/testutils"
	"golang.org/x/sys/unix"
)

func TestMain(m *testing.M) {
	runtime.LockOSThread()
	os.Exit(m.Run())
}

func terminateProcess(pid int) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}

	_ = proc.Kill()

	// We can't simply call proc.Wait, since it will hang when the thread leader exits while there are still subthreads.
	// By calling wait4 like below, it reaps the subthreads first and then reaps the thread leader.
	var status unix.WaitStatus
	for {
		if wpid, err := unix.Wait4(-1, &status, 0, nil); err != nil || wpid == pid {
			return
		}
	}
}

func TestLaunchProcess(t *testing.T) {
	client := NewClient()
	err := client.LaunchProcess(testutils.ProgramInfloop)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	// Do some ptrace action for testing
	_, err = syscall.PtracePeekData(pid, uintptr(testutils.InfloopAddrMain), []byte{0x0})
	if err != nil {
		t.Errorf("can't peek process' data: %v", err)
	}
}

func TestLaunchProcess_NonExistProgram(t *testing.T) {
	client := NewClient()
	err := client.LaunchProcess("notexist")
	if err == nil {
		t.Fatal("error is not returned")
	}

	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)
}

func TestAttachProcess(t *testing.T) {
	cmd := exec.Command(testutils.ProgramInfloop)
	_ = cmd.Start()
	pid := cmd.Process.Pid
	defer terminateProcess(pid)

	fmt.Println(pid)
	client := NewClient()
	err := client.AttachProcess(pid)
	if err != nil {
		t.Fatalf("failed to attach process: %v", err)
	}

	// Do some ptrace action for testing
	_, err = syscall.PtracePeekData(pid, uintptr(testutils.InfloopAddrMain), []byte{0x0})
	if err != nil {
		t.Errorf("can't peek process' data: %v", err)
	}
}

func TestAttachProcess_SignaledBeforeAttach(t *testing.T) {
	cmd := exec.Command(testutils.ProgramInfloop)
	_ = cmd.Start()
	pid := cmd.Process.Pid
	defer terminateProcess(pid)

	proc, _ := os.FindProcess(pid)
	_ = proc.Signal(unix.SIGUSR1)

	client := NewClient()
	err := client.AttachProcess(pid)
	if err == nil {
		t.Fatalf("error is not returned")
	}
}

func TestAttachProcess_NonExistPid(t *testing.T) {
	cmd := exec.Command(testutils.ProgramInfloop)
	_ = cmd.Start()
	pid := cmd.Process.Pid
	terminateProcess(pid) // make sure the pid doesn't exist

	client := NewClient()
	err := client.AttachProcess(pid)
	if err == nil {
		t.Fatalf("error is not returned")
	}
}

func TestDetachProcess(t *testing.T) {
	client := NewClient()
	err := client.LaunchProcess(testutils.ProgramInfloop)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	if err := client.DetachProcess(); err != nil {
		t.Fatalf("failed to detach process: %v", err)
	}

	// Do some ptrace action for testing
	_, err = syscall.PtracePeekData(pid, uintptr(testutils.InfloopAddrMain), []byte{0x0})
	if err == nil {
		t.Errorf("error is not returned")
	}
}

func TestReadMemory(t *testing.T) {
	client := NewClient()
	_ = client.LaunchProcess(testutils.ProgramInfloop)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	expected := []byte{0x64, 0x48, 0x8b}
	buff := make([]byte, len(expected))
	err := client.ReadMemory(pid, uintptr(testutils.InfloopAddrMain), buff)
	if err != nil {
		t.Fatalf("failed to read memory (pid: %d): %v", pid, err)
	}

	if !reflect.DeepEqual(buff, expected) {
		t.Errorf("Unexpected content: %v", buff)
	}
}

func TestWriteMemory(t *testing.T) {
	client := NewClient()
	_ = client.LaunchProcess(testutils.ProgramInfloop)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	softwareBreakpoint := []byte{0xcc}
	err := client.WriteMemory(pid, uintptr(testutils.InfloopAddrMain), softwareBreakpoint)
	if err != nil {
		t.Fatalf("failed to write memory (pid: %d): %v", pid, err)
	}
}

func TestReadRegisters(t *testing.T) {
	client := NewClient()
	_ = client.LaunchProcess(testutils.ProgramInfloop)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	regs, err := client.ReadRegisters(pid)
	if err != nil {
		t.Fatalf("failed to read registers (pid: %d): %v", pid, err)
	}

	if regs.Rip == 0 {
		t.Errorf("emptyr rip")
	}
}

func TestWriteRegisters(t *testing.T) {
	client := NewClient()
	_ = client.LaunchProcess(testutils.ProgramInfloop)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	regs, _ := client.ReadRegisters(pid)
	regs.Rip = uint64(testutils.InfloopAddrMain)
	err := client.WriteRegisters(pid, &regs)
	if err != nil {
		t.Fatalf("failed to write registers (pid: %d): %v", pid, err)
	}
}

func TestContinueAndWait_Trapped(t *testing.T) {
	client := NewClient()
	_ = client.LaunchProcess(testutils.ProgramInfloop)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	_ = client.WriteMemory(pid, uintptr(testutils.InfloopAddrMain), []byte{0xcc})
	event, err := client.ContinueAndWait(pid)
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	if event.Type != EventTypeTrapped {
		t.Fatalf("unexpected event: %#v", event.Type)
	}
	stoppedPID := event.Data.([]int)
	if stoppedPID[0] != pid {
		t.Fatalf("unexpected process is stopped: %d", stoppedPID[0])
	}
}

func TestContinueAndWait_Exited(t *testing.T) {
	client := NewClient()
	_ = client.LaunchProcess(testutils.ProgramHelloworld)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	wpid := pid
	for {
		event, err := client.ContinueAndWait(wpid)
		if err != nil {
			t.Fatalf("failed to continue and wait: %v", err)
		}
		if event.Type == EventTypeExited {
			break
		}
		wpid = event.Data.([]int)[0]
	}
}

func TestContinueAndWait_Signaled(t *testing.T) {
	client := NewClient()
	_ = client.LaunchProcess(testutils.ProgramHelloworld)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	proc, _ := os.FindProcess(pid)
	_ = proc.Signal(unix.SIGTERM)

	event, err := client.ContinueAndWait(pid)
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	expectedEvent := Event{Type: EventTypeTerminated, Data: int(unix.SIGTERM)}
	if event != expectedEvent {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestContinueAndWait_Stopped(t *testing.T) {
	client := NewClient()
	_ = client.LaunchProcess(testutils.ProgramHelloworld)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	proc, _ := os.FindProcess(pid)
	_ = proc.Signal(unix.SIGUSR1)

	// non-SIGTRAP signal is handled internally.
	_, err := client.ContinueAndWait(pid)
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
}

func TestContinueAndWait_CoreDump(t *testing.T) {
	client := NewClient()
	_ = client.LaunchProcess(testutils.ProgramHelloworld)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	proc, _ := os.FindProcess(pid)
	_ = proc.Signal(unix.SIGQUIT)

	event, err := client.ContinueAndWait(pid)
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	expectedEvent := Event{Type: EventTypeCoreDump}
	if event != expectedEvent {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestContinueAndWait_Continued(t *testing.T) {
	client := NewClient()
	_ = client.LaunchProcess(testutils.ProgramHelloworld)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	proc, _ := os.FindProcess(pid)
	_ = proc.Signal(unix.SIGCONT)

	_, err := client.ContinueAndWait(pid)
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
}

func TestContinueAndWait_WaitAllChildrenExit(t *testing.T) {
	client := NewClient()
	_ = client.LaunchProcess(testutils.ProgramHelloworld)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	pids := []int{pid}
	for {
		event, err := client.ContinueAndWait(pids...)
		if err != nil {
			t.Fatalf("failed to continue and wait: %v", err)
		}

		if event.Type == EventTypeExited {
			break
		}

		switch event.Type {
		case EventTypeExited:
			pids = nil
		default:
			pids = event.Data.([]int)
		}
	}
}

func TestStepAndWait(t *testing.T) {
	client := NewClient()
	_ = client.LaunchProcess(testutils.ProgramInfloop)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	event, err := client.StepAndWait(pid)
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	if event.Type != EventTypeTrapped {
		t.Fatalf("unexpected event type: %v", event.Type)
	}
	stoppedPID := event.Data.([]int)[0]
	if stoppedPID != pid {
		t.Fatalf("unexpected process is stopped: %d", stoppedPID)
	}
}
