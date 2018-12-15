package service

import (
	"errors"
	"log"
	"net"
	"net/rpc"

	"github.com/ks888/tgo/tracer"
)

const serviceVersion = 1 // increment whenever any changes are aded to service methods.

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
	// This parameter is required because the tracer may not have a chance to set the new trace points
	// after the attached tracee starts running without trace points.
	InitialStartTracePoint uint64
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
	t.controller.AddStartTracePoint(args.InitialStartTracePoint)

	go func() { t.errCh <- t.controller.MainLoop() }()
	return nil
}

// Detach lets the server detach from the attached process.
func (t *Tracer) Detach(args struct{}, reply *struct{}) error {
	if t.controller == nil {
		return nil
	}

	// TODO: the tracer may be killed before detached (and before breakpoints cleared). Implement the cancellation mechanism which can wait until the process is detached.
	t.controller.Interrupt()
	go func() {
		if err := <-t.errCh; err != nil {
			log.Printf("%v", err)
		}
		t.controller = nil
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
	tracer := &Tracer{errCh: make(chan error)}
	rpc.Register(tracer)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	// The server is running only for 1 client. So close the listener socket immediately and
	// do not create a new go routine for a new connection.
	conn, err := listener.Accept()
	listener.Close()
	if err != nil {
		return err
	}

	rpc.ServeConn(conn)
	conn.Close() // connection may be closed already
	return nil
}
