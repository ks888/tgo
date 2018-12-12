package service

import (
	"errors"
	"net"
	"net/rpc"

	"github.com/ks888/tgo/log"
	"github.com/ks888/tgo/tracer"
)

const serviceVersion = 1 // increment when the backward compacibility of service methods is broken.

// Tracer is the wrapper of the actual tracer in tgo/tracer package.
//
// The simple name 'Tracer' is chosen because it becomes a part of the service methods
// the rpc client uses.
type Tracer struct {
	controller *tracer.Controller
	errCh      chan error
}

// AttachArgs is the input argument of the service method 'Tracer.Attach'
type AttachArgs struct {
	Pid                    int
	TraceLevel, ParseLevel int
	Verbose                bool
}

// Version returns the service version. The backward compatibility may be broken if the version is not same as the expected one.
func (t *Tracer) Version(args struct{}, reply *int) error {
	*reply = serviceVersion
	return nil
}

// Attach lets the server attach to the specified process. It does nothing if the server is already attached.
func (t *Tracer) Attach(args AttachArgs, reply *struct{}) error {
	if t.controller != nil {
		return errors.New("already attached")
	}

	t.controller = tracer.NewController()
	if err := t.controller.AttachTracee(args.Pid); err != nil {
		return err
	}
	t.controller.SetTraceLevel(args.TraceLevel)
	t.controller.SetParseLevel(args.ParseLevel)

	go func() { t.errCh <- t.controller.MainLoop() }()
	return nil
}

// Detach lets the server detach from the attached process.
func (t *Tracer) Detach(args struct{}, reply *struct{}) error {
	if t.controller == nil {
		return nil
	}

	// TODO: the tracer may exit without clearing breakpoints. More rubust interrupt mechanism is necessary.
	go func() {
		t.controller.Interrupt()
		if err := <-t.errCh; err != nil {
			log.Printf("failed to detach: %v\n", err)
		}
	}()

	return nil
}

// AddStartTracePoint adds a new start trace point.
func (t *Tracer) AddStartTracePoint(args uint64, reply *struct{}) error {
	return t.controller.AddStartTracePoint(args)
}

// AddEndTracePoint adds a new end trace point.
func (t *Tracer) AddEndTracePoint(args uint64, reply *struct{}) error {
	return t.controller.AddEndTracePoint(args)
}

// Serve serves the tracer service.
func Serve(address string) error {
	wrapper := &Tracer{errCh: make(chan error)}
	rpc.Register(wrapper)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}

		// Handle only one client at once
		rpc.ServeConn(conn)
		conn.Close()
	}
}
