// +build darwin linux

package tracee

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ks888/tgo/testutils"
)

func TestFindProgramPath(t *testing.T) {
	cmd := exec.Command(testutils.ProgramInfloop)
	_ = cmd.Start()
	defer func() {
		cmd.Process.Kill()
		cmd.Process.Wait()
	}()

	path, err := findProgramPath(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("failed to find path: %v", err)
	}
	if path != testutils.ProgramInfloop {
		t.Errorf("wrong path: %s", path)
	}
}

func TestFindProgramPath_RelativePath(t *testing.T) {
	wd, _ := os.Getwd()
	relPath, _ := filepath.Rel(wd, testutils.ProgramInfloop)
	cmd := exec.Command(relPath)
	_ = cmd.Start()
	defer func() {
		cmd.Process.Kill()
		cmd.Process.Wait()
	}()

	path, err := findProgramPath(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("failed to find path: %v", err)
	}
	if path != testutils.ProgramInfloop {
		t.Errorf("wrong path: %s", path)
	}
}

func TestFindProgramPath_NoPid(t *testing.T) {
	cmd := exec.Command(testutils.ProgramHelloworld)
	_ = cmd.Run()

	_, err := findProgramPath(cmd.Process.Pid)
	if err == nil {
		t.Fatalf("error not returned")
	}
}
