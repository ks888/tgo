package tracer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ks888/tgo/testutils"
)

func TestStartStop(t *testing.T) {
	cmd := exec.Command(testutils.ProgramStartStop)
	out, _ := cmd.CombinedOutput()

	if strings.Count(string(out), "main.tracedFunc") != 2 {
		t.Errorf("unexpected output: %s", string(out))
	}
	if strings.Count(string(out), "fmt.Println") != 4 && strings.Count(string(out), "fmt.Fprintln") != 4 /* inlined */ {
		t.Errorf("unexpected output: %s", string(out))
	}
}

func TestStart(t *testing.T) {
	cmd := exec.Command(testutils.ProgramStartOnly)
	out, _ := cmd.CombinedOutput()

	if strings.Count(string(out), "main.f") != 2 {
		t.Errorf("unexpected output: %s", string(out))
	}
}

func TestStart_NoTracerBinary(t *testing.T) {
	origTracerName := tracerProgramName
	tracerProgramName = "not-exist-tracer"
	defer func() { tracerProgramName = origTracerName }()

	if err := Start(); err == nil {
		t.Fatalf("should return error")
	}
}

func TestMain(m *testing.M) {
	_, srcFilename, _, _ := runtime.Caller(0)
	srcDirname := filepath.Dir(srcFilename)
	outDirname := filepath.Join(srcDirname, "build")
	_ = os.Mkdir(outDirname, os.FileMode(0700)) // the directory may exist already

	if err := build(filepath.Join(srcDirname, "..", "..", "cmd", "tgo"), filepath.Join(outDirname, "tgo")); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build: %v\n", err)
	}

	orgPath := os.Getenv("PATH")
	os.Setenv("PATH", outDirname+string(os.PathListSeparator)+orgPath)

	exitStatus := m.Run()
	// the deferred function is not called when os.Exit()
	_ = os.RemoveAll(outDirname)
	os.Setenv("PATH", orgPath)
	os.Exit(exitStatus)
}

func build(mainPkgDirname, pathToBinary string) error {
	pkgPath, err := filepath.Rel(filepath.Join(os.Getenv("GOPATH"), "src"), mainPkgDirname)
	if err != nil {
		return err
	}

	args := []string{"build", "-o", pathToBinary, pkgPath}
	if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%v\n%s", err, string(out))
	}
	return nil
}
