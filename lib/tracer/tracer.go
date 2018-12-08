package tracer

import (
	"fmt"
	"net"
	"net/rpc"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/ks888/tgo/server"
)

var (
	client            *rpc.Client
	serverCmd         *exec.Cmd
	tracerProgramName = "tgo"
	// All exported functions must hold this lock.
	tracerLock sync.Mutex
)

// On enables the tracer. Ignored if the tracer is already enabled.
func On() error {
	tracerLock.Lock()
	defer tracerLock.Unlock()

	if serverCmd != nil {
		// The tracer is already enabled
		return nil
	}

	addr, err := startServer()
	if err != nil {
		return err
	}

	client, err = connectServer(addr)
	if err != nil {
		_ = terminateServer()
		return err
	}

	// TODO: specify tracelevel and parselevel options
	// TODO: Find proper function
	attachArgs := &server.AttachArgs{Pid: os.Getpid(), Function: "fmt.Println", TraceLevel: 1, ParseLevel: 1}
	if err := client.Call("Tracer.Attach", attachArgs, nil); err != nil {
		_ = terminateServer()
		return err
	}
	return nil
}

// Off disables the tracer. Ignored if the tracer is already disabled.
func Off() error {
	tracerLock.Lock()
	defer tracerLock.Unlock()

	if serverCmd == nil {
		// The tracer is already disabled
		return nil
	}

	if err := client.Call("Tracer.Detach", struct{}{}, nil); err != nil {
		_ = terminateServer()
		return err
	}

	return terminateServer()
}

func startServer() (string, error) {
	unusedPort, err := findUnusedPort()
	if err != nil {
		return "", fmt.Errorf("failed to find unused port: %v", err)
	}
	addr := fmt.Sprintf(":%d", unusedPort)

	args := []string{"server", addr}
	serverCmd = exec.Command(tracerProgramName, args...)
	serverCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // Otherwise, tracer may receive the signal to this process.
	serverCmd.Stdout = os.Stdout
	serverCmd.Stderr = os.Stderr
	if err := serverCmd.Start(); err != nil {
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

func terminateServer() error {
	defer func() { serverCmd = nil }()

	if err := client.Close(); err != nil {
		return err
	}
	if err := serverCmd.Process.Kill(); err != nil {
		return err
	}
	_, err := serverCmd.Process.Wait()
	return err
}
