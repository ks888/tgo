package tracer

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/ks888/tgo/testutils"
)

func TestOnAndOff(t *testing.T) {
	cmd := exec.Command(testutils.ProgramOnAndOff)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to execute command: %v", err)
	}

	if strings.Count(string(out), "fmt.Println") != 2 {
		t.Errorf("unexpected output: %s", string(out))
	}
}

func TestOn_NoTracerBinary(t *testing.T) {
	origTracerName := tracerProgramName
	tracerProgramName = "not-exist-tracer"
	defer func() { tracerProgramName = origTracerName }()

	if err := On(NewDefaultOption()); err == nil {
		t.Fatalf("should return error")
	}
}
