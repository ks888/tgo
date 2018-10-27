package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/ks888/tgo/tracer"
)

type options struct {
	function               string
	traceLevel, parseLevel int
}

func run(pid int, args []string, opts options) error {
	controller := tracer.NewController()
	if pid == 0 {
		if err := controller.LaunchTracee(args[0], args[1:]...); err != nil {
			return fmt.Errorf("failed to launch tracee: %v", err)
		}
	} else {
		if err := controller.AttachTracee(pid); err != nil {
			return fmt.Errorf("failed to attach tracee: %v", err)
		}
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	go func() {
		_ = <-ch
		controller.Interrupt()
	}()

	if err := controller.SetTracePoint(opts.function); err != nil {
		return err
	}
	controller.SetTraceLevel(opts.traceLevel)
	controller.SetParseLevel(opts.parseLevel)

	return controller.MainLoop()
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] [program] [args...]\n", os.Args[0])
		flag.PrintDefaults()
	}

	log.SetFlags(0)

	// TODO: use subcommand for the attach case
	pid := flag.Int("attach", 0, "The `pid` to attach")
	function := flag.String("func", "main.main", "The tracing is enabled when this `function` is called and then disabled when returned.")
	traceLevel := flag.Int("tracelevel", 1, "The function info is printed if the stack depth is within this `tracelevel`. The stack depth here is based on the point the tracing is enabled.")
	parseLevel := flag.Int("parselevel", 1, "The printed function info includes the value of args. The `parselevel` option determines how detailed these values should be.")
	flag.Parse()
	if *pid == 0 && flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}
	args := flag.Args()

	opts := options{function: *function, traceLevel: *traceLevel, parseLevel: *parseLevel}
	if err := run(*pid, args, opts); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
