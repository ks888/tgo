package tracer

import (
	"bytes"
	"io/ioutil"
	"os/exec"
	"strings"
	"testing"

	"github.com/ks888/tgo/testutils"
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
	defer cmd.Process.Kill()

	controller := NewController()
	err := controller.AttachTracee(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("failed to attch to the process: %v", err)
	}
}

func TestSetTracePoint(t *testing.T) {
	controller := NewController()
	err := controller.LaunchTracee(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	if err := controller.SetTracePoint("main.main"); err != nil {
		t.Errorf("failed to set tracing point: %v", err)
	}

	if !hasBreakpointAt(controller, "main.main") {
		t.Errorf("breakpoint is not set at main.main")
	}
}

func TestSetTracePoint_SetTwice(t *testing.T) {
	controller := NewController()
	err := controller.LaunchTracee(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	if err := controller.SetTracePoint("main.init"); err != nil {
		t.Errorf("failed to set tracing point: %v", err)
	}

	if err := controller.SetTracePoint("main.main"); err == nil {
		t.Errorf("error not returned")
	}
}

func hasBreakpointAt(controller *Controller, functionName string) bool {
	var addr uint64 = 0x0
	functions, _ := controller.process.Binary.ListFunctions()
	for _, function := range functions {
		if function.Name == functionName {
			addr = function.Value
		}
	}

	return controller.process.HasBreakpoint(addr)
}

func TestMainLoop_MainMain(t *testing.T) {
	controller := NewController()
	buff := &bytes.Buffer{}
	controller.outputWriter = buff
	if err := controller.LaunchTracee(testutils.ProgramHelloworld); err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	if err := controller.SetTracePoint("main.main"); err != nil {
		t.Fatalf("failed to set tracing point: %v", err)
	}

	if err := controller.MainLoop(); err != nil {
		t.Errorf("failed to run main loop: %v", err)
	}

	output := buff.String()
	if strings.Count(output, "main.main") != 2 {
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
	if err := controller.SetTracePoint("main.noParameter"); err != nil {
		t.Fatalf("failed to set tracing point: %v", err)
	}

	if err := controller.MainLoop(); err != nil {
		t.Errorf("failed to run main loop: %v", err)
	}

	output := buff.String()
	if strings.Count(output, "main.noParameter") != 2 {
		t.Errorf("not contain 'main.noParameter':\n%s", output)
	}
	if strings.Count(output, "main.oneParameter") != 0 {
		t.Errorf("contain 'main.oneParameter':\n%s", output)
	}
}

func TestMainLoop_GoRoutines(t *testing.T) {
	controller := NewController()
	buff := &bytes.Buffer{}
	controller.outputWriter = buff
	if err := controller.LaunchTracee(testutils.ProgramGoRoutines); err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	if err := controller.SetTracePoint("main.inc"); err != nil {
		t.Fatalf("failed to set tracing point: %v", err)
	}

	if err := controller.MainLoop(); err != nil {
		t.Errorf("failed to run main loop: %v", err)
	}

	output := buff.String()
	if strings.Count(output, "main.inc") != 20 {
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
	if err := controller.SetTracePoint("main.main"); err != nil {
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
	if err := controller.SetTracePoint("main.main"); err != nil {
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
	if err := controller.SetTracePoint("main.main"); err != nil {
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

func TestTracingPoint_EnterAndExit(t *testing.T) {
	point := tracingPoint{}
	point.Enter(1, 1)
	if !point.Inside(1) {
		t.Errorf("not inside")
	}

	point.Enter(1, 2)
	point.Exit(1, 2)
	if !point.Inside(1) {
		t.Errorf("not inside")
	}
	point.Exit(1, 1)
	if point.Inside(1) {
		t.Errorf("still inside")
	}
}
