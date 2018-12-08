package server

import (
	"net"
	"net/rpc"

	"github.com/ks888/tgo/log"
	"github.com/ks888/tgo/tracer"
)

// Tracer is the wrapper of the actual tracer in tgo/tracer package.
//
// Prefer the simple type name here because it becomes a part of 'serviceMethod'
// the rpc client uses.
type Tracer struct {
	Controller *tracer.Controller
}

type AttachArgs struct {
	Pid                    int
	Function               string
	TraceLevel, ParseLevel int
	Verbose                bool
}

func (tracer *Tracer) Attach(args AttachArgs, reply *struct{}) error {
	log.EnableDebugLog = args.Verbose

	if err := tracer.Controller.AttachTracee(args.Pid); err != nil {
		return err
	}
	tracer.Controller.SetTraceLevel(args.TraceLevel)
	tracer.Controller.SetParseLevel(args.ParseLevel)
	if err := tracer.Controller.SetTracePoint(args.Function); err != nil {
		return err
	}

	go tracer.Controller.MainLoop()

	return nil
}

func (tracer *Tracer) Detach(args struct{}, reply *struct{}) error {
	return nil
}

func Serve(address string) error {
	wrapper := &Tracer{}
	rpc.Register(wrapper)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()

	for {
		wrapper.Controller = tracer.NewController()
		conn, err := listener.Accept()
		if err != nil {
			return err
		}

		// Handle only one client at once
		rpc.ServeConn(conn)
		conn.Close()
	}
}
