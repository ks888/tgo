package os

import (
	"os"
	"reflect"
	"syscall"
	"testing"
)

var (
	infloopProgram                 = "testdata/infloop"
	addrTextSection        uintptr = 0x401000
	firstInstInTextSection         = []byte{0x48, 0x8b, 0x44, 0x24, 0x10}
)

func terminateProcess(pid int) {
	proc, err := os.FindProcess(pid)
	if err == nil {
		_ = proc.Kill()
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
	pid, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer terminateProcess(pid)

	buff := make([]byte, len(firstInstInTextSection))
	err = client.ReadMemory(pid, addrTextSection, buff)
	if err != nil {
		t.Fatalf("failed to read memory (pid: %d): %v", pid, err)
	}

	if !reflect.DeepEqual(buff, firstInstInTextSection) {
		t.Errorf("Unexpected content: %v", buff)
	}
}

func TestWriteMemory(t *testing.T) {
	client := NewClient()
	pid, err := client.LaunchProcess(infloopProgram)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer terminateProcess(pid)

	softwareBreakpoint := []byte{0xcc}
	err = client.WriteMemory(pid, addrTextSection, softwareBreakpoint)
	if err != nil {
		t.Fatalf("failed to write memory (pid: %d): %v", pid, err)
	}
}
