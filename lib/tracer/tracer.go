// Package tracer provides functions to start and stop tracing, as well as the options to change
// the tracer's behaviors.
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
	"unsafe" // For go:linkname

	"github.com/ks888/tgo/service"
)

const expectedVersion = 1

var (
	client            *rpc.Client
	serverCmd         *exec.Cmd
	tracerProgramName           = "tgo"
	traceLevel                  = 1
	parseLevel                  = 1
	verbose                     = false
	writer            io.Writer = os.Stdout
	errorWriter       io.Writer = os.Stderr
	// Protects the server command and its rpc client
	serverMtx sync.Mutex
)

//go:linkname firstModuleData runtime.firstmoduledata
var firstModuleData interface{}

// SetTraceLevel sets the trace level. Functions are traced if the stack depth is within this trace level. The stack depth here is based on the point tracing is enabled. The default is 1.
func SetTraceLevel(option int) {
	traceLevel = option
}

// SetParseLevel sets the parse level. The trace log includes the function's args. The parselevel option determines how detailed these values should be. The default is 1.
func SetParseLevel(option int) {
	parseLevel = option
}

// SetVerboseOption sets the verbose option. It true, the debug-level messages are written as well as the normal tracing log. The default is false.
func SetVerboseOption(option bool) {
	verbose = option
}

// SetWriter sets the writer for the tracing log. The default is os.Stdout.
func SetWriter(option io.Writer) {
	writer = option
}

// SetErrorWriter sets the writer for the error log. The default is os.Stderrr.
func SetErrorWriter(option io.Writer) {
	errorWriter = option
}

// Start enables tracing.
func Start() error {
	serverMtx.Lock()
	defer serverMtx.Unlock()

	pcs := make([]uintptr, 1)
	_ = runtime.Callers(2, pcs)
	startTracePoint := pcs[0]

	if serverCmd == nil {
		err := initialize(startTracePoint)
		if err != nil {
			_ = terminateServer()
			return fmt.Errorf("failed to start tracer: %v", err)
		}
		return nil
	}

	reply := &struct{}{} // sometimes the nil reply value causes panic even if the reply is not written.
	return client.Call("Tracer.AddStartTracePoint", startTracePoint, reply)
}

func initialize(startTracePoint uintptr) error {
	addr, err := startServer()
	if err != nil {
		return err
	}

	client, err = connectServer(addr)
	if err != nil {
		return err
	}

	if err := checkVersion(); err != nil {
		return err
	}

	programPath, err := os.Executable()
	if err != nil {
		return err
	}

	attachArgs := &service.AttachArgs{
		Pid:                    os.Getpid(),
		TraceLevel:             traceLevel,
		ParseLevel:             parseLevel,
		InitialStartTracePoint: startTracePoint,
		GoVersion:              runtime.Version(),
		ProgramPath:            programPath,
		FirstModuleDataAddr:    uintptr(unsafe.Pointer(&firstModuleData)),
	}
	reply := &struct{}{}
	if err := client.Call("Tracer.Attach", attachArgs, reply); err != nil {
		return err
	}

	stopFuncAddr := reflect.ValueOf(Stop).Pointer()
	return client.Call("Tracer.AddEndTracePoint", stopFuncAddr, reply)
}

func checkVersion() error {
	var serverVersion int
	if err := client.Call("Tracer.Version", struct{}{}, &serverVersion); err != nil {
		return err
	}
	if expectedVersion != serverVersion {
		return fmt.Errorf("the expected API version (%d) is not same as the actual API version (%d)", expectedVersion, serverVersion)
	}
	return nil
}

// Stop stops tracing.
//
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
	if verbose {
		args = append(args, "-verbose")
	}
	args = append(args, addr)
	serverCmd = exec.Command(tracerProgramName, args...)
	serverCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // Otherwise, tracer may receive the signal to this process.
	serverCmd.Stdout = writer
	serverCmd.Stderr = errorWriter
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

	if client != nil {
		if err := client.Close(); err != nil {
			return err
		}
	}

	if serverCmd != nil && serverCmd.Process != nil {
		if err := serverCmd.Process.Kill(); err != nil {
			return err
		}
		_, err := serverCmd.Process.Wait()
		return err
	}
	return nil
}
