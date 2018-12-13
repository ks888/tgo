package tracer

import (
	"fmt"
	"io"
	"net"
	"net/rpc"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/ks888/tgo/service"
)

var (
	client            *rpc.Client
	serverCmd         *exec.Cmd
	tracerProgramName = "tgo"
	option            = NewDefaultOption()
	// Protects the server command and its rpc client
	serverMtx sync.Mutex
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

// NewDefaultOption returns a new default option.
func NewDefaultOption() Option {
	return Option{TraceLevel: 1, ParseLevel: 1, Stdout: os.Stdout, Stderr: os.Stderr}
}

// SetOption sets the tracer option. It must be set before tracing starts.
func SetOption(o Option) {
	option = o
}

// Start enables tracing.
func Start() error {
	serverMtx.Lock()
	defer serverMtx.Unlock()

	pcs := make([]uintptr, 2)
	_ = runtime.Callers(2, pcs)
	startTracePoint, endTracePoint := uint64(pcs[0]), uint64(pcs[1])

	if serverCmd == nil {
		return initialize(startTracePoint, endTracePoint)
	}

	if err := client.Call("Tracer.AddStartTracePoint", startTracePoint, nil); err != nil {
		return err
	}
	return client.Call("Tracer.AddEndTracePoint", endTracePoint, nil)
}

func initialize(startTracePoint, endTracePoint uint64) error {
	addr, err := startServer()
	if err != nil {
		return err
	}

	client, err = connectServer(addr)
	if err != nil {
		_ = terminateServer()
		return err
	}

	attachArgs := &service.AttachArgs{
		Pid:                    os.Getpid(),
		TraceLevel:             option.TraceLevel,
		ParseLevel:             option.ParseLevel,
		InitialStartTracePoint: startTracePoint,
	}
	if err := client.Call("Tracer.Attach", attachArgs, nil); err != nil {
		_ = terminateServer()
		return err
	}

	if err := client.Call("Tracer.AddEndTracePoint", endTracePoint, nil); err != nil {
		_ = terminateServer()
		return err
	}

	stopFuncAddr := reflect.ValueOf(Stop).Pointer()
	if err := client.Call("Tracer.AddEndTracePoint", stopFuncAddr, nil); err != nil {
		_ = terminateServer()
		return err
	}
	return nil
}

// Stop stops tracing.
//go:noinline
func Stop() {
	return
}

func startServer() (string, error) {
	unusedPort, err := findUnusedPort()
	if err != nil {
		return "", fmt.Errorf("failed to find unused port: %v", err)
	}
	addr := fmt.Sprintf(":%d", unusedPort)

	args := []string{"server"}
	if option.Verbose {
		args = append(args, "-v")
	}
	args = append(args, addr)
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
