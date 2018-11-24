package tracee

import (
	"os"
	"os/exec"
	"testing"

	"github.com/ks888/tgo/testutils"
)

func TestLaunchProcess(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()
	if proc.debugapiClient == nil {
		t.Errorf("debugapiClient is nil")
	}
}

func TestAttachProcess(t *testing.T) {
	cmd := exec.Command(testutils.ProgramInfloop)
	_ = cmd.Start()

	proc, err := AttachProcess(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("failed to attach process: %v", err)
	}
	if proc.debugapiClient == nil {
		t.Errorf("debugapiClient is nil")
	}
	defer func() {
		proc.Detach() // must detach before kill. Otherwise, the program becomes zombie.
		cmd.Process.Kill()
		cmd.Process.Wait()
	}()
}

func TestDetach(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	if err := proc.SetBreakpoint(testutils.HelloworldAddrNoParameter); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if err := proc.Detach(); err != nil {
		t.Fatalf("failed to detach process: %v", err)
	}

	if proc.HasBreakpoint(testutils.HelloworldAddrNoParameter) {
		t.Errorf("breakpoint still exists")
	}
}

func TestContinueAndWait(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	// 1. stop at NoParameter func
	if err := proc.SetBreakpoint(testutils.HelloworldAddrNoParameter); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	event, err := proc.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	if err := proc.ClearBreakpoint(testutils.HelloworldAddrNoParameter); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	tids := event.Data.([]int)
	if err := proc.setPC(tids[0], testutils.HelloworldAddrNoParameter); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	// 2. stop at OneParameter func
	if err := proc.SetBreakpoint(testutils.HelloworldAddrOneParameter); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	if _, err := proc.ContinueAndWait(); err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	info, err := proc.CurrentGoRoutineInfo(tids[0])
	if err != nil {
		t.Fatalf("failed to get CurrentGoRoutineInfo: %v", err)
	}
	if info.CurrentPC-1 != testutils.HelloworldAddrOneParameter {
		t.Errorf("stop at unexpected address: %x", info.CurrentPC)
	}
}

func TestSingleStep(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	if err := proc.SetBreakpoint(testutils.HelloworldAddrNoParameter); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	event, err := proc.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}

	tids := event.Data.([]int)
	if err := proc.SingleStep(tids[0], testutils.HelloworldAddrNoParameter); err != nil {
		t.Fatalf("single-step failed: %v", err)
	}
	if !proc.HasBreakpoint(testutils.HelloworldAddrNoParameter) {
		t.Errorf("breakpoint is cleared")
	}
}

func TestSingleStep_NoBreakpoint(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	if err := proc.SetBreakpoint(testutils.HelloworldAddrNoParameter); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	event, err := proc.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}
	if err := proc.ClearBreakpoint(testutils.HelloworldAddrNoParameter); err != nil {
		t.Fatalf("failed to clear breakpoint: %v", err)
	}

	tids := event.Data.([]int)
	if err := proc.SingleStep(tids[0], testutils.HelloworldAddrNoParameter); err != nil {
		t.Fatalf("single-step failed: %v", err)
	}
	if proc.HasBreakpoint(testutils.HelloworldAddrNoParameter) {
		t.Errorf("breakpoint is set")
	}
}

func TestSetBreakpoint(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	err = proc.SetBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	buff := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(testutils.HelloworldAddrMain, buff)
	if buff[0] != 0xcc {
		t.Errorf("breakpoint is not set: %x", buff[0])
	}
}

func TestSetBreakpoint_AlreadySet(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	expected := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(testutils.HelloworldAddrMain, expected)

	err = proc.SetBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	err = proc.SetBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	err = proc.ClearBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Fatalf("failed to clear breakpoint: %v", err)
	}

	actual := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(testutils.HelloworldAddrMain, actual)
	if expected[0] != actual[0] {
		t.Errorf("the instruction is not restored: %x", actual)
	}
}

func TestSetConditionalBreakpoint(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	err = proc.SetConditionalBreakpoint(testutils.HelloworldAddrMain, 1)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	buff := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(testutils.HelloworldAddrMain, buff)
	if buff[0] != 0xcc {
		t.Errorf("breakpoint is not set: %x", buff[0])
	}
}

func TestSetConditionalBreakpoint_MultipleGoRoutines(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	if err := proc.SetConditionalBreakpoint(testutils.HelloworldAddrMain, 1); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if err := proc.SetConditionalBreakpoint(testutils.HelloworldAddrMain, 2); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if err = proc.ClearConditionalBreakpoint(testutils.HelloworldAddrMain, 2); err != nil {
		t.Fatalf("failed to clear breakpoint: %v", err)
	}
	if !proc.HasBreakpoint(testutils.HelloworldAddrMain) {
		t.Errorf("breakpoint is not set")
	}

	if err = proc.ClearConditionalBreakpoint(testutils.HelloworldAddrMain, 1); err != nil {
		t.Fatalf("failed to clear breakpoint: %v", err)
	}
	if proc.HasBreakpoint(testutils.HelloworldAddrMain) {
		t.Errorf("breakpoint is set")
	}
}

func TestClearBreakpoint(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	expected := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(testutils.HelloworldAddrMain, expected)

	err = proc.SetBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}
	err = proc.ClearBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Fatalf("failed to clear breakpoint: %v", err)
	}

	actual := make([]byte, 1)
	_ = proc.debugapiClient.ReadMemory(testutils.HelloworldAddrMain, actual)
	if expected[0] != actual[0] {
		t.Errorf("the instruction is not restored: %x", actual)
	}
}

func TestClearBreakpoint_BPNotSet(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	err = proc.ClearBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Errorf("failed to clear breakpoint: %v", err)
	}
}

func TestHitBreakpoint(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	err = proc.SetConditionalBreakpoint(testutils.HelloworldAddrMain, 1)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if !proc.HitBreakpoint(testutils.HelloworldAddrMain, 1) {
		t.Errorf("invalid condition check")
	}

	if proc.HitBreakpoint(testutils.HelloworldAddrMain, 2) {
		t.Errorf("invalid condition check")
	}
}

func TestHitBreakpoint_NoCondition(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	err = proc.SetBreakpoint(testutils.HelloworldAddrMain)
	if err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if !proc.HitBreakpoint(testutils.HelloworldAddrMain, 1) {
		t.Errorf("invalid condition check")
	}
}

func TestStackFrameAt(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	if err := proc.SetBreakpoint(testutils.HelloworldAddrOneParameterAndVariable); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	event, err := proc.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}

	tids := event.Data.([]int)
	regs, err := proc.debugapiClient.ReadRegisters(tids[0])
	if err != nil {
		t.Fatalf("failed to read registers: %v", err)
	}

	stackFrame, err := proc.StackFrameAt(regs.Rsp, regs.Rip)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if stackFrame.Function.Name != "main.oneParameterAndOneVariable" {
		t.Errorf("wrong function name: %s", stackFrame.Function.Name)
	}
	if stackFrame.ReturnAddress == 0x0 {
		t.Errorf("empty return address")
	}
	if len(stackFrame.InputArguments) != 1 {
		t.Errorf("wrong input args length: %d", len(stackFrame.InputArguments))
	}
	if stackFrame.InputArguments[0].ParseValue(1) != "i = 1" {
		t.Errorf("wrong input args: %s", stackFrame.InputArguments[0].ParseValue(1))
	}
	if len(stackFrame.OutputArguments) != 0 {
		t.Errorf("wrong output args length: %d", len(stackFrame.OutputArguments))
	}
}

func TestCurrentGoRoutineInfo(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	if err := proc.SetBreakpoint(testutils.HelloworldAddrMain); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	event, err := proc.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}

	tids := event.Data.([]int)
	goRoutineInfo, err := proc.CurrentGoRoutineInfo(tids[0])
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if goRoutineInfo.ID != 1 {
		t.Errorf("wrong id: %d", goRoutineInfo.ID)
	}
	if goRoutineInfo.UsedStackSize == 0 {
		t.Errorf("wrong stack size: %d", goRoutineInfo.UsedStackSize)
	}
	if goRoutineInfo.CurrentPC != testutils.HelloworldAddrMain+1 {
		t.Errorf("empty return address: %d", goRoutineInfo.CurrentPC)
	}
	if goRoutineInfo.CurrentStackAddr == 0 {
		t.Errorf("empty stack address: %d", goRoutineInfo.CurrentStackAddr)
	}
	if goRoutineInfo.Panicking {
		t.Errorf("panicking")
	}
	// main go routine always has 'defer' setting. See runtime.main() for the detail.
	if goRoutineInfo.PanicHandler == nil || goRoutineInfo.PanicHandler.PCAtDefer == 0 || goRoutineInfo.PanicHandler.UsedStackSizeAtDefer == 0 {
		t.Errorf("deferedBy is nil or its value is 0")
	}
}

func TestCurrentGoRoutineInfo_Panicking(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramPanic)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	if err := proc.SetBreakpoint(testutils.PanicAddrInsideThrough); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	event, err := proc.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}

	tids := event.Data.([]int)
	goRoutineInfo, err := proc.CurrentGoRoutineInfo(tids[0])
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !goRoutineInfo.Panicking {
		t.Errorf("not panicking")
	}

	function, _ := proc.Binary.FindFunction(goRoutineInfo.PanicHandler.PCAtDefer)
	if function.Name != "main.g" {
		t.Errorf("wrong panic handler")
	}
}

func TestCurrentGoRoutineInfo_HasAncestors(t *testing.T) {
	os.Setenv("GODEBUG", "tracebackancestors=1")
	defer os.Unsetenv("GODEBUG")

	proc, err := LaunchProcess(testutils.ProgramGoRoutines)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	if !proc.Binary.GoVersion.LaterThan(GoVersion{MajorVersion: 1, MinorVersion: 11, PatchVersion: 0}) {
		t.Skip("go 1.10 or earlier doesn't hold ancestors info")
	}

	if err := proc.SetBreakpoint(testutils.GoRoutinesAddrInc); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	event, err := proc.ContinueAndWait()
	if err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}

	tids := event.Data.([]int)
	goRoutineInfo, err := proc.CurrentGoRoutineInfo(tids[0])
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if goRoutineInfo.Ancestors == nil {
		t.Errorf("nil ancestors")
	}
}
