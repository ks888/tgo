package os

import (
	"os"
	"reflect"
	"runtime"
	"syscall"
	"testing"
	"time"

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

	// We can't simply call proc.Wait, since it will hang when the thread leader exits while there are still other threads.
	// By calling wait4 syscall like below, it waits any of the threads until the thread leader exits.
	var status unix.WaitStatus
	for {
		if wpid, err := unix.Wait4(-1, &status, unix.WALL|unix.WNOHANG, nil); err != nil || wpid == pid {
			return
		}
		time.Sleep(10 * time.Millisecond)
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

func TestProcess_NonExistProgram(t *testing.T) {
	client := NewClient()
	pid, err := client.LaunchProcess("notexist")
	if err == nil {
		t.Fatal("error is not returned")
	}
	defer terminateProcess(pid)
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

	var regs unix.PtraceRegs
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

	var regs unix.PtraceRegs
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

// waitがhangしている
// func TestContinueAndWait_Exited(t *testing.T) {
// 	client := NewClient()
// 	pid, _ := client.LaunchProcess(helloworldProgram)
// 	defer terminateProcess(pid)

// 	wpid := pid
// 	for {
// 		stoppedPID, event, err := client.ContinueAndWait(wpid)
// 		if err != nil {
// 			t.Fatalf("failed to continue and wait: %v", err)
// 		}
// 		if event.Type == debugapi.EventTypeExited {
// 			break
// 		}
// 		fmt.Printf("pid: %d, event: %#v\n", stoppedPID, event)
// 		wpid = stoppedPID
// 	}
// }

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
	pid, _ := client.LaunchProcess(infloopProgram)
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

func TestContinueAndWait_CoreDump(t *testing.T) {
	client := NewClient()
	pid, _ := client.LaunchProcess(infloopProgram)
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

// func TestContinueAndWait_WaitAllChildren(t *testing.T) {
// 	client := NewClient()
// 	pid, _ := client.LaunchProcess(infloopProgram)
// 	defer terminateProcess(pid)

// 	pids := make(map[int]bool)
// 	for len(pids) < 2 {
// 		wpid, _, err := client.ContinueAndWait(pid)
// 		if err != nil {
// 			t.Fatalf("failed to continue and wait: %v", err)
// 		}
// 		pids[wpid] = true
// 		fmt.Println("pid:", wpid)
// 	}
// }
