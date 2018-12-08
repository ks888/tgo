package tracer

import (
	"fmt"
	"io"
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

// Option is the options for tracer.
type Option struct {
	// Functions are traced if the stack depth is within this tracelevel. The stack depth here is based on the point the tracing is enabled.
	TraceLevel int
	// The trace log includes the function's args. The parselevel option determines how detailed these values should be.
	ParseLevel int
	// Show the debug-level message
	Verbose bool
	// Deliver tracer's stdout to this writer.
	Stdout io.Writer
	// Deliver tracer's stderr to this writer.
	Stderr io.Writer
}

func NewDefaultOption() Option {
	return Option{TraceLevel: 1, ParseLevel: 1, Stdout: os.Stdout, Stderr: os.Stderr}
}

// On enables the tracer. Ignored if the tracer is already enabled.
func On(option Option) error {
	tracerLock.Lock()
	defer tracerLock.Unlock()

	if serverCmd != nil {
		// The tracer is already enabled
		return nil
	}

	addr, err := startServer(option)
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
	attachArgs := &server.AttachArgs{
		Pid:        os.Getpid(),
		Function:   "fmt.Println",
		TraceLevel: option.TraceLevel,
		ParseLevel: option.ParseLevel,
		Verbose:    option.Verbose,
	}
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

func startServer(option Option) (string, error) {
	unusedPort, err := findUnusedPort()
	if err != nil {
		return "", fmt.Errorf("failed to find unused port: %v", err)
	}
	addr := fmt.Sprintf(":%d", unusedPort)

	args := []string{"server", addr}
	serverCmd = exec.Command(tracerProgramName, args...)
	serverCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // Otherwise, tracer may receive the signal to this process.
	serverCmd.Stdout = option.Stdout
	serverCmd.Stderr = option.Stderr
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
