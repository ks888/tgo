package tracee

import (
	"debug/dwarf"
	"os/exec"
	"runtime"
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

	proc, err := AttachProcess(cmd.Process.Pid, testutils.ProgramInfloop, runtime.Version())
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

	if proc.ExistBreakpoint(testutils.HelloworldAddrNoParameter) {
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
	if !proc.ExistBreakpoint(testutils.HelloworldAddrNoParameter) {
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
	if proc.ExistBreakpoint(testutils.HelloworldAddrNoParameter) {
		t.Errorf("breakpoint is set")
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
	if stackFrame.Function.StartAddr != testutils.HelloworldAddrOneParameterAndVariable {
		t.Errorf("start addr is 0")
	}
	if stackFrame.Function.EndAddr == 0 {
		t.Errorf("end addr is 0")
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

func TestStackFrameAt_NoDwarfCase(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworldNoDwarf)
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
	if stackFrame.Function.StartAddr != testutils.HelloworldAddrOneParameterAndVariable {
		t.Errorf("wrong function value: %#x", stackFrame.Function.StartAddr)
	}
	if stackFrame.Function.EndAddr == 0 {
		t.Errorf("end addr is 0")
	}
	if len(stackFrame.Function.Parameters) != 1 {
		t.Errorf("wrong number of params")
	}
}

func TestFindFunction_FillInCheck(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	if err := proc.SetBreakpoint(testutils.HelloworldAddrMain); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if _, err := proc.ContinueAndWait(); err != nil {
		t.Fatalf("failed to continue and wait: %v", err)
	}

	f, err := proc.FindFunction(testutils.HelloworldAddrOneParameter)
	if err != nil {
		t.Fatalf("failed to find func: %v", err)
	}

	numNotExist := 0
	numOffset0 := 0
	for _, param := range f.Parameters {
		if !param.Exist {
			numNotExist++
		}
		if param.Offset == 0 {
			numOffset0++
		}
	}
	if numNotExist == 1 {
		t.Errorf("The number of NonExist parameter is 1, params: %#v", f.Parameters)
	}
	if numOffset0 != 1 {
		t.Errorf("The number of offset 0 parameter is %d, params: %#v", numOffset0, f.Parameters)
	}
}

func TestFuncTypeOffsets(t *testing.T) {
	binary, _ := OpenBinaryFile(testutils.ProgramHelloworld, GoVersion{})
	debuggableBinary, _ := binary.(debuggableBinaryFile)

	entry, err := debuggableBinary.findDWARFEntryByName(func(entry *dwarf.Entry) bool {
		if entry.Tag != dwarf.TagStructType {
			return false
		}
		name, err := stringClassAttr(entry, dwarf.AttrName)
		return name == "runtime._func" && err == nil
	})
	if err != nil {
		t.Fatalf("no _func type entry: %v", err)
	}

	expectedFuncType, err := debuggableBinary.dwarf.Type(entry.Offset)
	if err != nil {
		t.Fatalf("no func type: %v", err)
	}

	expectedFields := expectedFuncType.(*dwarf.StructType).Field
	for _, actualField := range _funcType.Field {
		for _, expectedField := range expectedFields {
			if actualField.Name == expectedField.Name {
				if actualField.ByteOffset != expectedField.ByteOffset {
					t.Errorf("wrong byte offset. expect: %d, actual: %d", expectedField.ByteOffset, actualField.ByteOffset)
				}
				if actualField.Type.Size() != expectedField.Type.Size() {
					t.Errorf("wrong size. expect: %d, actual: %d", expectedField.Type.Size(), actualField.Type.Size())
				}
				break
			}
		}
	}
}

func TestFindfuncbucketTypeOffsets(t *testing.T) {
	if !ParseGoVersion(runtime.Version()).LaterThan(GoVersion{MajorVersion: 1, MinorVersion: 11}) {
		t.Skip("go1.10 or earlier doesn't have findfuncbucket type in DWARF")
	}

	binary, _ := OpenBinaryFile(testutils.ProgramHelloworld, GoVersion{})
	debuggableBinary, _ := binary.(debuggableBinaryFile)

	entry, err := debuggableBinary.findDWARFEntryByName(func(entry *dwarf.Entry) bool {
		if entry.Tag != dwarf.TagStructType {
			return false
		}
		name, err := stringClassAttr(entry, dwarf.AttrName)
		return name == "runtime.findfuncbucket" && err == nil
	})
	if err != nil {
		t.Fatalf("no findfuncbucket type entry: %v", err)
	}

	expectedFindfuncbucketType, err := debuggableBinary.dwarf.Type(entry.Offset)
	if err != nil {
		t.Fatalf("no findfuncbucket type: %v", err)
	}

	expectedFields := expectedFindfuncbucketType.(*dwarf.StructType).Field
	for _, actualField := range findfuncbucketType.Field {
		for _, expectedField := range expectedFields {
			if actualField.Name == expectedField.Name {
				if actualField.ByteOffset != expectedField.ByteOffset {
					t.Errorf("wrong byte offset. expect: %d, actual: %d", expectedField.ByteOffset, actualField.ByteOffset)
				}
				if actualField.Type.Size() != expectedField.Type.Size() {
					t.Errorf("wrong size. expect: %d, actual: %d", expectedField.Type.Size(), actualField.Type.Size())
				}
				break
			}
		}
	}
}

func TestReadInstructions(t *testing.T) {
	for _, testdata := range []struct {
		program  string
		funcAddr uint64
		targetOS string // specify if the func is not available in some OSes. Keep it empty otherwise.
	}{
		{testutils.ProgramHelloworld, testutils.HelloworldAddrMain, ""},
		{testutils.ProgramHelloworldNoDwarf, testutils.HelloworldAddrMain, ""},     // includes the last 0xcc insts
		{testutils.ProgramHelloworld, testutils.HelloworldAddrGoBuildID, "darwin"}, // includes bad insts
	} {
		if testdata.targetOS != "" && testdata.targetOS != runtime.GOOS {
			continue
		}
		proc, err := LaunchProcess(testdata.program)
		if err != nil {
			t.Fatalf("failed to launch process: %v", err)
		}
		defer proc.Detach()

		f, err := proc.FindFunction(testdata.funcAddr)
		if err != nil {
			t.Fatalf("failed to find function: %v", err)
		}
		insts, err := proc.ReadInstructions(f)
		if err != nil {
			t.Fatalf("failed to read instructions: %v", err)
		}

		if len(insts) == 0 {
			t.Errorf("empty insts")
		}
	}
}

func TestCurrentGoRoutineInfo(t *testing.T) {
	for i, testProgram := range []string{testutils.ProgramHelloworld, testutils.ProgramHelloworldNoDwarf} {
		proc, err := LaunchProcess(testProgram)
		if err != nil {
			t.Fatalf("[%d] failed to launch process: %v", i, err)
		}
		defer proc.Detach()

		if err := proc.SetBreakpoint(testutils.HelloworldAddrMain); err != nil {
			t.Fatalf("[%d] failed to set breakpoint: %v", i, err)
		}

		event, err := proc.ContinueAndWait()
		if err != nil {
			t.Fatalf("[%d] failed to continue and wait: %v", i, err)
		}

		threadIDs := event.Data.([]int)
		goRoutineInfo, err := proc.CurrentGoRoutineInfo(threadIDs[0])
		if err != nil {
			t.Fatalf("[%d] error: %v", i, err)
		}
		if goRoutineInfo.ID != 1 {
			t.Errorf("[%d] wrong id: %d", i, goRoutineInfo.ID)
		}
		if goRoutineInfo.UsedStackSize == 0 {
			t.Errorf("[%d] wrong stack size: %d", i, goRoutineInfo.UsedStackSize)
		}
		if goRoutineInfo.CurrentPC != testutils.HelloworldAddrMain+1 {
			t.Errorf("[%d] wrong pc: %d", i, goRoutineInfo.CurrentPC)
		}
		if goRoutineInfo.CurrentStackAddr == 0 {
			t.Errorf("[%d] current stack address is 0", i)
		}
		if goRoutineInfo.NextDeferFuncAddr == 0 {
			t.Errorf("[%d] NextDeferFuncAddr is 0", i)
		}
		if goRoutineInfo.Panicking {
			t.Errorf("[%d] panicking", i)
		}
		// main go routine always has 'defer' setting. See runtime.main() for the detail.
		if goRoutineInfo.PanicHandler == nil || goRoutineInfo.PanicHandler.PCAtDefer == 0 || goRoutineInfo.PanicHandler.UsedStackSizeAtDefer == 0 {
			t.Errorf("[%d] deferedBy is nil or its value is 0", i)
		}
	}
}

func TestCurrentGoRoutineInfo_Panicking(t *testing.T) {
	for _, testProgram := range []string{testutils.ProgramPanic, testutils.ProgramPanicNoDwarf} {
		proc, err := LaunchProcess(testProgram)
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

		if goRoutineInfo.PanicHandler.PCAtDefer == 0 {
			t.Errorf("invalid panic handler")
		}
	}
}

func TestArgument_ParseValue(t *testing.T) {
	for i, testdata := range []struct {
		arg      Argument
		expected string
	}{
		{Argument{Name: "a", parseValue: func(int) value { return int8Value{val: 1} }}, "a = 1"},
		{Argument{Name: "a", parseValue: func(int) value { return nil }}, "a = -"},
		{Argument{Name: "", parseValue: func(int) value { return int8Value{val: 1} }}, "1"},
	} {
		actual := testdata.arg.ParseValue(0)
		if actual != testdata.expected {
			t.Errorf("[%d] wrong parsed result. expect: %s, actual %s", i, testdata.expected, actual)
		}
	}

}
