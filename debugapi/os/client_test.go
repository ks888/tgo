package os

import (
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"syscall"
	"testing"

	"github.com/ks888/tgo/debugapi"
	"golang.org/x/sys/unix"
)

var (
	infloopProgram                 = "testdata/infloop"
	entryPoint             uintptr = 0x448f80
	addrTextSection        uintptr = 0x401000
	firstInstInTextSection         = []byte{0x48, 0x8b, 0x44, 0x24, 0x10}

	helloworldProgram = "testdata/helloworld"
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
	pid, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer terminateProcess(pid)

	// Do some ptrace action for testing
	_, err = syscall.PtracePeekData(pid, addrTextSection, []byte{0x0})
	if err != nil {
		t.Errorf("can't peek process' data: %v", err)
	}
}

func TestLaunchProcess_NonExistProgram(t *testing.T) {
	client := NewClient()
	pid, err := client.LaunchProcess("notexist")
	if err == nil {
		t.Fatal("error is not returned")
	}
	defer terminateProcess(pid)
}

func TestAttachProcess(t *testing.T) {
	cmd := exec.Command(infloopProgram)
	_ = cmd.Start()
	pid := cmd.Process.Pid
	defer terminateProcess(pid)

	client := NewClient()
	err := client.AttachProcess(pid)
	if err != nil {
		t.Fatalf("failed to attach process: %v", err)
	}

	// Do some ptrace action for testing
	_, err = syscall.PtracePeekData(pid, addrTextSection, []byte{0x0})
	if err != nil {
		t.Errorf("can't peek process' data: %v", err)
	}
}

func TestAttachProcess_SignaledBeforeAttach(t *testing.T) {
	cmd := exec.Command(infloopProgram)
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
	cmd := exec.Command(infloopProgram)
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
	pid, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer terminateProcess(pid)

	err = client.DetachProcess(pid)
	if err != nil {
		t.Fatalf("failed to detach process: %v", err)
	}

	// Do some ptrace action for testing
	_, err = syscall.PtracePeekData(pid, addrTextSection, []byte{0x0})
	if err == nil {
		t.Errorf("error is not returned")
	}
}

func TestDetachProcess_DetachChildrenImplicitly(t *testing.T) {
	client := NewClient()
	pid, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer terminateProcess(pid)

	var childPid int
	for {
		_, event, err := client.ContinueAndWait(pid)
		if err != nil {
			t.Fatalf("failed to continue and wait: %v", err)
		}
		if event.Type == debugapi.EventTypeCreated && event.Data != pid {
			childPid = event.Data
			break
		}
	}

	err = client.DetachProcess(pid)
	if err != nil {
		t.Fatalf("failed to detach process: %v", err)
	}

	// Do some ptrace action for testing
	_, err = syscall.PtracePeekData(childPid, addrTextSection, []byte{0x0})
	if err == nil {
		t.Errorf("error is not returned")
	}
}

func TestReadMemory(t *testing.T) {
	client := NewClient()
	pid, _ := client.LaunchProcess(infloopProgram)
	defer terminateProcess(pid)

	buff := make([]byte, len(firstInstInTextSection))
	err := client.ReadMemory(pid, addrTextSection, buff)
	if err != nil {
		t.Fatalf("failed to read memory (pid: %d): %v", pid, err)
	}

	if !reflect.DeepEqual(buff, firstInstInTextSection) {
		t.Errorf("Unexpected content: %v", buff)
	}
}

func TestWriteMemory(t *testing.T) {
	client := NewClient()
	pid, _ := client.LaunchProcess(infloopProgram)
	defer terminateProcess(pid)

	softwareBreakpoint := []byte{0xcc}
	err := client.WriteMemory(pid, addrTextSection, softwareBreakpoint)
	if err != nil {
		t.Fatalf("failed to write memory (pid: %d): %v", pid, err)
	}
}

func TestReadRegisters(t *testing.T) {
	client := NewClient()
	pid, _ := client.LaunchProcess(infloopProgram)
	defer terminateProcess(pid)

	var regs debugapi.Registers
	err := client.ReadRegisters(pid, &regs)
	if err != nil {
		t.Fatalf("failed to read registers (pid: %d): %v", pid, err)
	}

	if regs.Rip != uint64(entryPoint) {
		t.Errorf("read registers have unexpected content: %d", regs.Rip)
	}
}

func TestWriteRegisters(t *testing.T) {
	client := NewClient()
	pid, _ := client.LaunchProcess(infloopProgram)
	defer terminateProcess(pid)

	var regs debugapi.Registers
	_ = client.ReadRegisters(pid, &regs)

	regs.Rip = uint64(addrTextSection)
	err := client.WriteRegisters(pid, &regs)
	if err != nil {
		t.Fatalf("failed to write registers (pid: %d): %v", pid, err)
	}
}

func TestContinueAndWait_Trapped(t *testing.T) {
	client := NewClient()
	pid, _ := client.LaunchProcess(infloopProgram)
	defer terminateProcess(pid)

	_ = client.WriteMemory(pid, entryPoint, []byte{0xcc})
	stoppedPID, event, err := client.ContinueAndWait(pid)
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	if stoppedPID != pid {
		t.Fatalf("unexpected process is stopped: %d", stoppedPID)
	}
	expectedEvent := debugapi.Event{Type: debugapi.EventTypeTrapped}
	if event != expectedEvent {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestContinueAndWait_Exited(t *testing.T) {
	client := NewClient()
	pid, _ := client.LaunchProcess(helloworldProgram)
	defer terminateProcess(pid)

	wpid := pid
	for {
		stoppedPID, event, err := client.ContinueAndWait(wpid)
		if err != nil {
			t.Fatalf("failed to continue and wait: %v", err)
		}
		if event.Type == debugapi.EventTypeExited {
			break
		}
		wpid = stoppedPID
	}
}

func TestContinueAndWait_ThreadCreated(t *testing.T) {
	client := NewClient()
	pid, _ := client.LaunchProcess(helloworldProgram)
	defer terminateProcess(pid)

	for {
		_, event, err := client.ContinueAndWait(pid)
		if err != nil {
			t.Fatalf("failed to continue and wait: %v", err)
		}
		if event.Type == debugapi.EventTypeCreated && event.Data != pid {
			break
		}
	}
}

func TestContinueAndWait_Signaled(t *testing.T) {
	client := NewClient()
	pid, _ := client.LaunchProcess(helloworldProgram)
	defer terminateProcess(pid)

	proc, _ := os.FindProcess(pid)
	_ = proc.Signal(unix.SIGTERM)

	signaledPID, event, err := client.ContinueAndWait(pid)
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	if signaledPID != pid {
		t.Fatalf("unexpected process is stopped: %d", signaledPID)
	}
	expectedEvent := debugapi.Event{Type: debugapi.EventTypeTerminated, Data: int(unix.SIGTERM)}
	if event != expectedEvent {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestContinueAndWait_Stopped(t *testing.T) {
	client := NewClient()
	pid, _ := client.LaunchProcess(helloworldProgram)
	defer terminateProcess(pid)

	proc, _ := os.FindProcess(pid)
	_ = proc.Signal(unix.SIGUSR1)

	// non-SIGTRAP signal is handled internally.
	_, _, err := client.ContinueAndWait(pid)
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
}

func TestContinueAndWait_CoreDump(t *testing.T) {
	client := NewClient()
	pid, _ := client.LaunchProcess(helloworldProgram)
	defer terminateProcess(pid)

	proc, _ := os.FindProcess(pid)
	_ = proc.Signal(unix.SIGQUIT)

	coreDumpPID, event, err := client.ContinueAndWait(pid)
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	if coreDumpPID != pid {
		t.Fatalf("unexpected process id: %d", coreDumpPID)
	}
	expectedEvent := debugapi.Event{Type: debugapi.EventTypeCoreDump}
	if event != expectedEvent {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestContinueAndWait_Continued(t *testing.T) {
	client := NewClient()
	pid, _ := client.LaunchProcess(helloworldProgram)
	defer terminateProcess(pid)

	proc, _ := os.FindProcess(pid)
	_ = proc.Signal(unix.SIGCONT)

	_, _, err := client.ContinueAndWait(pid)
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
}

func TestContinueAndWait_WaitAllChildrenExit(t *testing.T) {
	client := NewClient()
	pid, _ := client.LaunchProcess(helloworldProgram)
	defer terminateProcess(pid)

	pids := []int{pid}
	for {
		wpid, event, err := client.ContinueAndWait(pids...)
		if err != nil {
			t.Fatalf("failed to continue and wait: %v", err)
		}

		if wpid == pid && event.Type == debugapi.EventTypeExited {
			break
		}

		switch event.Type {
		case debugapi.EventTypeExited:
			pids = nil
		default:
			pids = []int{wpid}
		}
	}
}

func TestStepAndWait(t *testing.T) {
	client := NewClient()
	pid, _ := client.LaunchProcess(infloopProgram)
	defer terminateProcess(pid)

	stoppedPID, event, err := client.StepAndWait(pid)
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	if stoppedPID != pid {
		t.Fatalf("unexpected process is stopped: %d", stoppedPID)
	}
	expectedEvent := debugapi.Event{Type: debugapi.EventTypeTrapped}
	if event != expectedEvent {
		t.Fatalf("unexpected event: %#v", event)
	}
}
