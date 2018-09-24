package tracer

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"
)

var (
	testdataHelloworld   = "testdata/helloworld"
	testdataHelloworldGo = testdataHelloworld + ".go"
)

func TestMain(m *testing.M) {
	if err := buildTestProgram(); err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func buildTestProgram() error {
	if out, err := exec.Command("go", "build", "-o", testdataHelloworld, testdataHelloworldGo).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build %s: %v\n%v", testdataHelloworldGo, err, string(out))
	}
	return nil
}

func TestLaunchProcess(t *testing.T) {
	controller := NewController()
	err := controller.LaunchTracee(testdataHelloworld)
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
	if err := controller.LaunchTracee(testdataHelloworld); err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	if err := controller.MainLoop(); err != nil {
		t.Errorf("failed to run main loop: %v", err)
	}

	output, _ := ioutil.ReadAll(buff)
	if strings.Count(string(output), "main.main") != 2 {
		t.Errorf("unexpected output: %s", string(output))
	}
}

func TestInterrupt(t *testing.T) {
	controller := NewController()
	controller.outputWriter = ioutil.Discard
	err := controller.LaunchTracee(testdataHelloworld)
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
