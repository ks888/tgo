package tracer

import (
	"fmt"
	"os"
	"os/exec"
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
	controller := &Controller{}
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

func TestInterrupt(t *testing.T) {
	controller := &Controller{}
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
