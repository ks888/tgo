// +build darwin linux

package tracee

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func findProgramPath(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	commandName := strings.TrimSpace(string(out))
	if filepath.IsAbs(commandName) {
		return commandName, nil
	}
	commandNameBase := filepath.Base(commandName)

	out, err = exec.Command("lsof", "-d", "txt", "-a", "-F", "n", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "n") {
			continue
		}

		absFileName := line[1:len(line)]
		if strings.HasSuffix(absFileName, commandNameBase) {
			return absFileName, nil
		}
	}
	return commandName, nil
}
