package tracer

import (
	"fmt"
	"net"
	"net/rpc"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/ks888/tgo/server"
)

var (
	client            *rpc.Client
	tracerCmd         *exec.Cmd
	tracerProgramName = "tgo"
)

// TODO: use mutex to allow only one go routine to access these functions at a time.

// On enables the tracer.
func On() error {
	addr, err := startServer()
	if err != nil {
		return err
	}

	client, err := connectServer(addr)
	if err != nil {
		return err
	}

	attachArgs := &server.AttachArgs{Pid: os.Getpid(), Function: "fmt.Println", TraceLevel: 1, ParseLevel: 1}
	return client.Call("Tracer.Attach", attachArgs, nil)
}

// Off disables the tracer.
func Off() error {
	if err := client.Call("Tracer.Detach", struct{}{}, nil); err != nil {
		return err
	}

	if err := client.Close(); err != nil {
		return err
	}

	return killServer()
}

func startServer() (string, error) {
	unusedPort, err := findUnusedPort()
	if err != nil {
		return "", fmt.Errorf("failed to find unused port: %v", err)
	}
	addr := fmt.Sprintf(":%d", unusedPort)

	args := []string{"server", addr}
	tracerCmd = exec.Command(tracerProgramName, args...)
	tracerCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // Otherwise, tracer may receive the signal to this process.
	tracerCmd.Stdout = os.Stdout
	tracerCmd.Stderr = os.Stderr
	if err := tracerCmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start server: %v", err)
	}
	return addr, nil
}

func findUnusedPort() (int, error) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{})
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port, nil
}

func connectServer(addr string) (*rpc.Client, error) {
	const numRetries = 5
	interval := 100 * time.Millisecond
	var err error
	for i := 0; i < numRetries; i++ {
		client, err = rpc.Dial("tcp", addr)
		if err == nil {
			return client, nil
		}

		time.Sleep(interval)
		interval *= 2
	}
	return nil, fmt.Errorf("can't connect to the server (addr: %s): %v", addr, err)
}

func killServer() error {
	defer func() { tracerCmd = nil }()

	if err := tracerCmd.Process.Kill(); err != nil {
		return err
	}
	_, err := tracerCmd.Process.Wait()
	return err
}
