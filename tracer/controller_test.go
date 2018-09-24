package tracer

import (
	"bytes"
	"io/ioutil"
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

	var addrMain uint64 = 0x0
	var addrMorestack uint64 = 0x0
	functions, _ := controller.process.Binary.ListFunctions()
	for _, function := range functions {
		if function.Name == "main.main" {
			addrMain = function.Value
		} else if function.Name == "runtime.morestack" {
			addrMorestack = function.Value
		}
	}

	if !controller.process.HasBreakpoint(addrMain) {
		t.Errorf("breakpoint is not set at main.main")
	}

	if controller.process.HasBreakpoint(addrMorestack) {
		t.Errorf("breakpoint is set at runtime.morestack")
	}
}

func TestMainLoop(t *testing.T) {
	controller := NewController()
	buff := &bytes.Buffer{}
	controller.outputWriter = buff
	if err := controller.LaunchTracee(testutils.ProgramHelloworld); err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	if err := controller.MainLoop(); err != nil {
		t.Errorf("failed to run main loop: %v", err)
	}

	output := buff.String()
	if strings.Count(output, "main.main") != 2 {
		t.Errorf("unexpected output: %s", output)
	}
}

func TestInterrupt(t *testing.T) {
	controller := NewController()
	controller.outputWriter = ioutil.Discard
	err := controller.LaunchTracee(testutils.ProgramInfloop)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
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
