package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"

	"github.com/ks888/tgo/log"
	"github.com/ks888/tgo/tracer"
)

type options struct {
	function               string
	traceLevel, parseLevel int
}

func launch(opts options) error {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage:

	%s launch [program] [program args]
`, os.Args[0])
	}

	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(1)
	}
	args := flag.Args()

	controller := tracer.NewController()
	if err := controller.LaunchTracee(args[1], args[2:]...); err != nil {
		return fmt.Errorf("failed to launch tracee: %v", err)
	}

	if err := setUpController(controller, opts); err != nil {
		return fmt.Errorf("failed to set up the controller: %v", err)
	}

	return controller.MainLoop()
}

func attach(opts options) error {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage:

	%s attach [pid]
`, os.Args[0])
	}

	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(1)
	}
	pid, err := strconv.Atoi(flag.Arg(1))
	if err != nil {
		return fmt.Errorf("invalid pid: %v", err)
	}

	controller := tracer.NewController()
	if err := controller.AttachTracee(pid); err != nil {
		return fmt.Errorf("failed to attach tracee: %v", err)
	}

	if err := setUpController(controller, opts); err != nil {
		return fmt.Errorf("failed to set up the controller: %v", err)
	}

	return controller.MainLoop()
}

func setUpController(controller *tracer.Controller, opts options) error {
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
	return nil
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `tgo is the function tracer for Go programs.

Usage:

	%s [flags] <command> [command arguments]

Commands:

	launch   launches and traces a new process
	attach   attaches to the exisiting process

Flags:
`, os.Args[0])
		flag.PrintDefaults()
	}

	function := flag.String("trace", "main.main", "The tracing is enabled when this `function` is called and then disabled when returned.")
	traceLevel := flag.Int("tracelevel", 1, "Functions are traced if the stack depth is within this `tracelevel` when the function is called. The stack depth here is based on the point the tracing is enabled.")
	parseLevel := flag.Int("parselevel", 1, "The trace log includes the function's args. The `parselevel` option determines how detailed these values should be.")
	verbose := flag.Bool("verbose", false, "Show the logging message")
	flag.Parse()

	log.EnableDebugLog = *verbose
	opts := options{function: *function, traceLevel: *traceLevel, parseLevel: *parseLevel}

	// TODO: use 3rd party package to manage subcommands.
	var err error
	switch flag.Arg(0) {
	case "launch":
		err = launch(opts)
	case "attach":
		err = attach(opts)
	default:
		flag.Usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
