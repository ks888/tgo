package server

import (
	"fmt"
	"net"
	"net/rpc"
	"os"

	"github.com/ks888/tgo/log"
	"github.com/ks888/tgo/tracer"
)

// Tracer is the wrapper of the actual tracer in tgo/tracer package.
//
// Prefer the simple type name here because it becomes a part of 'serviceMethod'
// the rpc client uses.
type Tracer struct {
	controller *tracer.Controller
	errCh      chan error
}

type AttachArgs struct {
	Pid                    int
	Function               string
	TraceLevel, ParseLevel int
	Verbose                bool
}

func (t *Tracer) Attach(args AttachArgs, reply *struct{}) error {
	log.EnableDebugLog = args.Verbose

	if err := t.controller.AttachTracee(args.Pid); err != nil {
		return err
	}
	t.controller.SetTraceLevel(args.TraceLevel)
	t.controller.SetParseLevel(args.ParseLevel)
	if err := t.controller.SetTracePoint(args.Function); err != nil {
		return err
	}

	go func() { t.errCh <- t.controller.MainLoop() }()

	return nil
}

func (t *Tracer) Detach(args struct{}, reply *struct{}) error {
	// TODO: the tracer may exit without clearing breakpoints. More rubust interrupt mechanism is necessary.
	go func() {
		t.controller.Interrupt()
		if err := <-t.errCh; err != nil {
			fmt.Fprintf(os.Stderr, "failed to detach: %v\n", err)
		}
	}()

	return nil
}

func Serve(address string) error {
	wrapper := &Tracer{errCh: make(chan error)}
	rpc.Register(wrapper)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()

	for {
		wrapper.controller = tracer.NewController()
		conn, err := listener.Accept()
		if err != nil {
			return err
		}

		// Handle only one client at once
		rpc.ServeConn(conn)
		conn.Close()
	}
}
