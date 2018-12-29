package debugapi

import (
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

func TestCheckInterface(t *testing.T) {
	var _ client = newRawClient()
	var _ client = NewClient()
}

func TestClientProxy(t *testing.T) {
	client := NewClient()
	_ = client.LaunchProcess(testutils.ProgramInfloop)
	pid := client.raw.tracingThreadIDs[0]
	defer terminateProcess(pid)

	_ = client.WriteMemory(testutils.InfloopAddrMain, []byte{0xcc})
	event, err := client.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	if event.Type != EventTypeTrapped {
		t.Fatalf("unexpected event: %#v", event.Type)
	}
}

func TestLaunchProcess(t *testing.T) {
	client := newRawClient()
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

func TestLaunchProcess_ProgramNotExist(t *testing.T) {
	client := newRawClient()
	err := client.LaunchProcess("notexist")
	if err == nil {
		t.Fatal("error is not returned")
	}
}

func TestAttachProcess(t *testing.T) {
	cmd := exec.Command(testutils.ProgramInfloop)
	_ = cmd.Start()
	pid := cmd.Process.Pid
	defer terminateProcess(pid)

	client := newRawClient()
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

	client := newRawClient()
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

	client := newRawClient()
	err := client.AttachProcess(pid)
	if err == nil {
		t.Fatalf("error is not returned")
	}
}

func TestDetachProcess(t *testing.T) {
	client := newRawClient()
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
	client := newRawClient()
	_ = client.LaunchProcess(testutils.ProgramInfloop)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	expected := []byte{0x64, 0x48, 0x8b}
	buff := make([]byte, len(expected))
	err := client.ReadMemory(testutils.InfloopAddrMain, buff)
	if err != nil {
		t.Fatalf("failed to read memory (pid: %d): %v", pid, err)
	}

	if !reflect.DeepEqual(buff, expected) {
		t.Errorf("Unexpected content: %v", buff)
	}
}

func TestWriteMemory(t *testing.T) {
	client := newRawClient()
	_ = client.LaunchProcess(testutils.ProgramInfloop)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	softwareBreakpoint := []byte{0xcc}
	err := client.WriteMemory(testutils.InfloopAddrMain, softwareBreakpoint)
	if err != nil {
		t.Fatalf("failed to write memory (pid: %d): %v", pid, err)
	}
}

func TestReadRegisters(t *testing.T) {
	client := newRawClient()
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
	client := newRawClient()
	_ = client.LaunchProcess(testutils.ProgramInfloop)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	regs, _ := client.ReadRegisters(pid)
	regs.Rip = uint64(testutils.InfloopAddrMain)
	err := client.WriteRegisters(pid, regs)
	if err != nil {
		t.Fatalf("failed to write registers (pid: %d): %v", pid, err)
	}
}

func TestReadTLS(t *testing.T) {
	client := newRawClient()
	err := client.LaunchProcess(testutils.ProgramInfloop)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	_ = client.WriteMemory(testutils.InfloopAddrMain, []byte{0xcc})
	_, _ = client.ContinueAndWait()

	var offset uint32 = 0xf8ffffff
	gAddr, err := client.ReadTLS(client.trappedThreadIDs[0], offset)
	if err != nil {
		t.Fatalf("failed to read tls: %v", err)
	}
	if gAddr == 0 {
		t.Errorf("empty addr")
	}
}

func TestContinueAndWait_Trapped(t *testing.T) {
	client := newRawClient()
	_ = client.LaunchProcess(testutils.ProgramInfloop)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	_ = client.WriteMemory(testutils.InfloopAddrMain, []byte{0xcc})
	event, err := client.ContinueAndWait()
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
	client := newRawClient()
	_ = client.LaunchProcess(testutils.ProgramHelloworld)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	for {
		event, err := client.ContinueAndWait()
		if err != nil {
			t.Fatalf("failed to continue and wait: %v", err)
		}
		if event.Type == EventTypeExited {
			break
		}
	}
}

func TestContinueAndWait_Signaled(t *testing.T) {
	client := newRawClient()
	_ = client.LaunchProcess(testutils.ProgramHelloworld)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	proc, _ := os.FindProcess(pid)
	_ = proc.Signal(unix.SIGTERM)

	event, err := client.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	expectedEvent := Event{Type: EventTypeTerminated, Data: int(unix.SIGTERM)}
	if event != expectedEvent {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestContinueAndWait_Stopped(t *testing.T) {
	client := newRawClient()
	_ = client.LaunchProcess(testutils.ProgramHelloworld)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	proc, _ := os.FindProcess(pid)
	_ = proc.Signal(unix.SIGUSR1)

	// non-SIGTRAP signal is handled internally.
	_, err := client.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
}

func TestContinueAndWait_CoreDump(t *testing.T) {
	client := newRawClient()
	_ = client.LaunchProcess(testutils.ProgramHelloworld)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	proc, _ := os.FindProcess(pid)
	_ = proc.Signal(unix.SIGQUIT)

	event, err := client.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	expectedEvent := Event{Type: EventTypeCoreDump}
	if event != expectedEvent {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestContinueAndWait_Continued(t *testing.T) {
	client := newRawClient()
	_ = client.LaunchProcess(testutils.ProgramHelloworld)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	proc, _ := os.FindProcess(pid)
	_ = proc.Signal(unix.SIGCONT)

	_, err := client.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
}

func TestContinueAndWait_WaitAllChildrenExit(t *testing.T) {
	client := newRawClient()
	_ = client.LaunchProcess(testutils.ProgramHelloworld)
	pid := client.tracingThreadIDs[0]
	defer terminateProcess(pid)

	for {
		event, err := client.ContinueAndWait()
		if err != nil {
			t.Fatalf("failed to continue and wait: %v", err)
		}

		if event.Type == EventTypeExited {
			break
		}
	}
}

func TestStepAndWait(t *testing.T) {
	client := newRawClient()
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
