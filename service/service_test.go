package service

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/ks888/tgo/testutils"
)

func TestAttachAndDetach(t *testing.T) {
	cmd := exec.Command(testutils.ProgramInfloop)
	_ = cmd.Start()

	tracer := &Tracer{}
	args := AttachArgs{
		Pid:                    cmd.Process.Pid,
		InitialStartTracePoint: uintptr(testutils.InfloopAddrMain),
		ProgramPath:            testutils.ProgramInfloop,
		GoVersion:              runtime.Version(),
	}
	if err := tracer.Attach(args, nil); err != nil {
		t.Errorf("failed to attach: %v", err)
	}

	if err := tracer.Detach(struct{}{}, nil); err != nil {
		t.Errorf("failed to detach: %v", err)
	}

	cmd.Process.Kill()
	cmd.Process.Wait()
}

func TestServe(t *testing.T) {
	unusedPort, err := findUnusedPort()
	if err != nil {
		t.Fatalf("failed to find unused port: %v", err)
	}
	addr := fmt.Sprintf(":%d", unusedPort)

	errCh := make(chan error)
	go func() {
		errCh <- Serve(addr)
	}()

	conn, err := connect(addr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	conn.Close()

	err = <-errCh
	if err != nil {
		t.Fatalf("failed to serve: %v", err)
	}
}

func findUnusedPort() (int, error) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{})
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port, nil
}

func connect(addr string) (net.Conn, error) {
	const numRetries = 5
	interval := 100 * time.Millisecond
	var err error
	for i := 0; i < numRetries; i++ {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			return conn, nil
		}

		time.Sleep(interval)
		interval *= 2
	}
	return nil, fmt.Errorf("can't connect to the server (addr: %s): %v", addr, err)
}
