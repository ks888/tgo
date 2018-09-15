package tracee

import (
	"testing"

	"github.com/ks888/tgo/debugapi/lldb"
)

func TestLaunchProcess(t *testing.T) {
	proc, err := LaunchProcess(testdataParameters)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	if proc.debugapiClient == nil {
		t.Errorf("debugapiClient is nil")
	}
	if proc.currentThreadID == 0 {
		t.Errorf("currentThreadID is 0")
	}
}

func TestSetBreakpoint(t *testing.T) {
	proc, err := LaunchProcess(testdataParameters)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	err = proc.SetBreakpoint(addrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	buff := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(addrMain, buff)
	if buff[0] != 0xcc {
		t.Errorf("breakpoint is not set: %x", buff[0])
	}
}

func TestSetBreakpoint_AlreadySet(t *testing.T) {
	proc, err := LaunchProcess(testdataParameters)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	expected := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(addrMain, expected)

	err = proc.SetBreakpoint(addrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	err = proc.SetBreakpoint(addrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	err = proc.ClearBreakpoint(addrMain)
	if err != nil {
		t.Fatalf("failed to clear breakpoint: %v", err)
	}

	actual := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(addrMain, actual)
	if expected[0] != actual[0] {
		t.Errorf("the instruction is not restored: %x", actual)
	}
}

func TestSetConditionalBreakpoint(t *testing.T) {
	proc, err := LaunchProcess(testdataParameters)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	err = proc.SetConditionalBreakpoint(addrMain, 1)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	buff := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(addrMain, buff)
	if buff[0] != 0xcc {
		t.Errorf("breakpoint is not set: %x", buff[0])
	}
}

func TestClearBreakpoint(t *testing.T) {
	proc, err := LaunchProcess(testdataParameters)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	expected := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(addrMain, expected)

	err = proc.SetBreakpoint(addrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	err = proc.ClearBreakpoint(addrMain)
	if err != nil {
		t.Fatalf("failed to clear breakpoint: %v", err)
	}

	actual := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(addrMain, actual)
	if expected[0] != actual[0] {
		t.Errorf("the instruction is not restored: %x", actual)
	}
}

func TestClearBreakpoint_BPNotSet(t *testing.T) {
	proc, err := LaunchProcess(testdataParameters)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	err = proc.ClearBreakpoint(addrMain)
	if err != nil {
		t.Errorf("failed to clear breakpoint: %v", err)
	}
}

func TestHitBreakpoint(t *testing.T) {
	proc, err := LaunchProcess(testdataParameters)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	err = proc.SetConditionalBreakpoint(addrMain, 1)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if !proc.HitBreakpoint(addrMain, 1) {
		t.Errorf("invalid condition check")
	}

	if proc.HitBreakpoint(addrMain, 2) {
		t.Errorf("invalid condition check")
	}
}

func TestHitBreakpoint_NoCondition(t *testing.T) {
	proc, err := LaunchProcess(testdataParameters)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	err = proc.SetBreakpoint(addrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if !proc.HitBreakpoint(addrMain, 1) {
		t.Errorf("invalid condition check")
	}
}

func TestCurrentGoRoutineID(t *testing.T) {
	client := lldb.NewClient()
	tid, err := client.LaunchProcess(testdataParameters)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	if err := client.WriteMemory(addrMain, []byte{0xcc}); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	if _, _, err = client.ContinueAndWait(); err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}

	proc := &Process{debugapiClient: client, currentThreadID: tid}
	id, err := proc.currentGoRoutineID()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if id != 1 {
		t.Errorf("wrong id: %d", id)
	}
}
