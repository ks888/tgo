package tracer

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ks888/tgo/testutils"
	"github.com/ks888/tgo/tracee"
)

func TestLaunchProcess(t *testing.T) {
	controller := NewController()
	err := controller.LaunchTracee(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
}

func TestAttachProcess(t *testing.T) {
	cmd := exec.Command(testutils.ProgramInfloop)
	_ = cmd.Start()

	controller := NewController()
	err := controller.AttachTracee(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("failed to attch to the process: %v", err)
	}

	controller.process.Detach() // must detach before kill. Otherwise, the program becomes zombie.
	cmd.Process.Kill()
	cmd.Process.Wait()
}

func TestAddStartTracePoint(t *testing.T) {
	controller := NewController()
	err := controller.LaunchTracee(testutils.ProgramStartStop)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	if err := controller.AddStartTracePoint(testutils.StartStopAddrTracedFunc); err != nil {
		t.Errorf("failed to set tracing point: %v", err)
	}
	if err := controller.setPendingTracePoints(); err != nil {
		t.Errorf("failed to set pending trace points: %v", err)
	}
	if !hasBreakpointAt(controller, "main.tracedFunc") {
		t.Errorf("breakpoint is not set at main.tracedFunc")
	}

	if err := controller.AddStartTracePoint(testutils.StartStopAddrTracedFunc); err != nil {
		t.Errorf("failed to set tracing point: %v", err)
	}
}

func TestAddEndTracePoint(t *testing.T) {
	controller := NewController()
	err := controller.LaunchTracee(testutils.ProgramStartStop)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	if err := controller.AddEndTracePoint(testutils.StartStopAddrTracedFunc); err != nil {
		t.Errorf("failed to set tracing point: %v", err)
	}
	if err := controller.setPendingTracePoints(); err != nil {
		t.Errorf("failed to set pending trace points: %v", err)
	}
	if !hasBreakpointAt(controller, "main.tracedFunc") {
		t.Errorf("breakpoint is not set at main.tracedFunc")
	}

	if err := controller.AddEndTracePoint(testutils.StartStopAddrTracedFunc); err != nil {
		t.Errorf("failed to set tracing point: %v", err)
	}
}

func hasBreakpointAt(controller *Controller, functionName string) bool {
	f, err := controller.findFunction(functionName)
	if err != nil {
		return false
	}

	return controller.process.HasBreakpoint(f.Value)
}

func TestMainLoop_MainMain(t *testing.T) {
	controller := NewController()
	buff := &bytes.Buffer{}
	controller.outputWriter = buff
	controller.SetTraceLevel(1)
	if err := controller.LaunchTracee(testutils.ProgramHelloworld); err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	if err := controller.AddStartTracePoint(testutils.HelloworldAddrMain); err != nil {
		t.Fatalf("failed to set tracing point: %v", err)
	}

	if err := controller.MainLoop(); err != nil {
		t.Errorf("failed to run main loop: %v", err)
	}

	output := buff.String()
	if strings.Count(output, "main.main") != 0 {
		t.Errorf("unexpected output: %s", output)
	}
	if strings.Count(output, "main.noParameter") != 2 {
		t.Errorf("unexpected output: %s", output)
	}
}

func TestMainLoop_MainNoParameter(t *testing.T) {
	controller := NewController()
	buff := &bytes.Buffer{}
	controller.outputWriter = buff
	if err := controller.LaunchTracee(testutils.ProgramHelloworld); err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	controller.SetTraceLevel(1)
	if err := controller.AddStartTracePoint(testutils.HelloworldAddrNoParameter); err != nil {
		t.Fatalf("failed to set tracing point: %v", err)
	}
	if err := controller.AddEndTracePoint(testutils.HelloworldAddrOneParameter); err != nil {
		t.Fatalf("failed to set tracing point: %v", err)
	}

	if err := controller.MainLoop(); err != nil {
		t.Errorf("failed to run main loop: %v", err)
	}

	output := buff.String()
	if strings.Count(output, "fmt.Println") != 2 {
		t.Errorf("unexpected output: %s", output)
	}
	if strings.Count(output, "main.noParameter") != 0 {
		t.Errorf("unexpected output: %s", output)
	}
	if strings.Count(output, "main.oneParameter") != 0 {
		t.Errorf("unexpected output: %s", output)
	}
}

func TestMainLoop_GoRoutines_FollowChildren(t *testing.T) {
	os.Setenv("GODEBUG", "tracebackancestors=1")
	defer os.Unsetenv("GODEBUG")

	controller := NewController()
	buff := &bytes.Buffer{}
	controller.outputWriter = buff
	controller.SetTraceLevel(1)
	if err := controller.LaunchTracee(testutils.ProgramGoRoutines); err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	if err := controller.AddStartTracePoint(testutils.GoRoutinesAddrMain); err != nil {
		t.Fatalf("failed to set tracing point: %v", err)
	}
	if !controller.process.Binary.CompiledGoVersion().LaterThan(tracee.GoVersion{MajorVersion: 1, MinorVersion: 11, PatchVersion: 0}) {
		t.Skip("go 1.10 or earlier doesn't hold ancestors info")
	}

	if err := controller.MainLoop(); err != nil {
		t.Errorf("failed to run main loop: %v", err)
	}

	output := buff.String()
	if strings.Count(output, "main.inc") != 40 {
		t.Errorf("unexpected output: %s", output)
	}
}

func TestMainLoop_Recursive(t *testing.T) {
	controller := NewController()
	buff := &bytes.Buffer{}
	controller.outputWriter = buff
	if err := controller.LaunchTracee(testutils.ProgramRecursive); err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	if err := controller.AddStartTracePoint(testutils.RecursiveAddrMain); err != nil {
		t.Fatalf("failed to set tracing point: %v", err)
	}
	controller.SetTraceLevel(3)

	if err := controller.MainLoop(); err != nil {
		t.Errorf("failed to run main loop: %v", err)
	}

	output := buff.String()
	if strings.Count(output, "main.dec") != 6 {
		t.Errorf("wrong number of main.dec: %d", strings.Count(output, "main.dec"))
	}
}

func TestMainLoop_Panic(t *testing.T) {
	controller := NewController()
	buff := &bytes.Buffer{}
	controller.outputWriter = buff
	if err := controller.LaunchTracee(testutils.ProgramPanic); err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	if err := controller.AddStartTracePoint(testutils.PanicAddrMain); err != nil {
		t.Fatalf("failed to set tracing point: %v", err)
	}
	controller.SetTraceLevel(2)

	if err := controller.MainLoop(); err != nil {
		t.Errorf("failed to run main loop: %v", err)
	}

	output := buff.String()
	if strings.Count(output, "main.catch") != 2 {
		t.Errorf("wrong number of main.catch: %d", strings.Count(output, "main.catch"))
	}
}

func TestInterrupt(t *testing.T) {
	controller := NewController()
	controller.outputWriter = ioutil.Discard
	err := controller.LaunchTracee(testutils.ProgramInfloop)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	if err := controller.AddStartTracePoint(testutils.InfloopAddrMain); err != nil {
		t.Fatalf("failed to set tracing point: %v", err)
	}

	done := make(chan error)
	go func(ch chan error) {
		ch <- controller.MainLoop()
	}(done)

	controller.Interrupt()
	if err := <-done; err != ErrInterrupted {
		t.Errorf("not interrupted: %v", err)
	}
}
