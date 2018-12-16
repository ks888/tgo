package tracer

import (
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
	if strings.Count(string(out), "fmt.Println") != 2 {
		t.Errorf("unexpected output: %s", string(out))
	}
}

func TestStart(t *testing.T) {
	cmd := exec.Command(testutils.ProgramStartOnly)
	out, _ := cmd.CombinedOutput()

	if strings.Count(string(out), "fmt.Println") != 2 {
		t.Errorf("unexpected output: %s", string(out))
	}
}

func TestOn_NoTracerBinary(t *testing.T) {
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
	buildDirname := filepath.Join(srcDirname, "build")
	_ = os.Mkdir(buildDirname, os.FileMode(0700)) // the directory may exist already

	testBinaryName := filepath.Join(buildDirname, "tgo")
	args := []string{"build", "-o", testBinaryName, filepath.Join(srcDirname, "..", "..", "cmd", "tgo")}
	if err := exec.Command("go", args...).Run(); err != nil {
		panic(err)
	}
	orgPath := os.Getenv("PATH")
	os.Setenv("PATH", buildDirname+string(os.PathListSeparator)+orgPath)

	exitStatus := m.Run()
	// the deferred function is not called when os.Exit()
	_ = os.RemoveAll(buildDirname)
	os.Setenv("PATH", orgPath)
	os.Exit(exitStatus)
}
