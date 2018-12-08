package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"

	"github.com/ks888/tgo/log"
	"github.com/ks888/tgo/server"
	"github.com/ks888/tgo/tracer"
)

const (
	traceOptionDesc      = "The tracing is enabled when this `function` is called and then disabled when returned."
	tracelevelOptionDesc = "Functions are traced if the stack depth is within this `tracelevel`. The stack depth here is based on the point the tracing is enabled."
	parselevelOptionDesc = "The trace log includes the function's args. The `parselevel` option determines how detailed these values should be."
	verboseOptionDesc    = "Show the debug-level message"
)

type options struct {
	function               string
	traceLevel, parseLevel int
}

func launchCmd(args []string) error {
	commandLine := flag.NewFlagSet("", flag.ExitOnError)
	commandLine.Usage = func() {
		fmt.Fprintf(commandLine.Output(), `Usage:

  %s launch [flags] [program name & args]

Flags:
`, os.Args[0])
		commandLine.PrintDefaults()
	}

	function := commandLine.String("trace", "main.main", traceOptionDesc)
	traceLevel := commandLine.Int("tracelevel", 1, tracelevelOptionDesc)
	parseLevel := commandLine.Int("parselevel", 1, parselevelOptionDesc)
	verbose := commandLine.Bool("verbose", false, verboseOptionDesc)

	commandLine.Parse(args)
	if commandLine.NArg() < 1 {
		commandLine.Usage()
		os.Exit(1)
	}
	log.EnableDebugLog = *verbose

	controller := tracer.NewController()
	if err := controller.LaunchTracee(commandLine.Args()[0], commandLine.Args()[1:]...); err != nil {
		return fmt.Errorf("failed to launch tracee: %v", err)
	}

	opts := options{function: *function, traceLevel: *traceLevel, parseLevel: *parseLevel}
	if err := setUpController(controller, opts); err != nil {
		return fmt.Errorf("failed to set up the controller: %v", err)
	}

	return controller.MainLoop()
}

func attachCmd(args []string) error {
	commandLine := flag.NewFlagSet("", flag.ExitOnError)
	commandLine.Usage = func() {
		fmt.Fprintf(commandLine.Output(), `Usage:

  %s attach [flags] [pid]

Flags:
`, os.Args[0])
		commandLine.PrintDefaults()
	}

	function := commandLine.String("trace", "main.main", traceOptionDesc)
	traceLevel := commandLine.Int("tracelevel", 1, tracelevelOptionDesc)
	parseLevel := commandLine.Int("parselevel", 1, parselevelOptionDesc)
	verbose := commandLine.Bool("verbose", false, verboseOptionDesc)

	commandLine.Parse(args)
	if commandLine.NArg() < 1 {
		commandLine.Usage()
		os.Exit(1)
	}
	log.EnableDebugLog = *verbose

	pid, err := strconv.Atoi(commandLine.Arg(0))
	if err != nil {
		return fmt.Errorf("invalid pid: %v", err)
	}

	controller := tracer.NewController()
	if err := controller.AttachTracee(pid); err != nil {
		return fmt.Errorf("failed to attach tracee: %v", err)
	}

	opts := options{function: *function, traceLevel: *traceLevel, parseLevel: *parseLevel}
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

func serverCmd(args []string) error {
	commandLine := flag.NewFlagSet("", flag.ExitOnError)
	commandLine.Usage = func() {
		fmt.Fprintf(commandLine.Output(), `Usage:

  %s server [hostname:port]

Flags:
`, os.Args[0])
		commandLine.PrintDefaults()
	}

	commandLine.Parse(args)
	if commandLine.NArg() < 1 {
		commandLine.Usage()
		os.Exit(1)
	}

	return server.Serve(commandLine.Arg(0))
}

func main() {
	commandLine := flag.NewFlagSet("", flag.ExitOnError)
	commandLine.Usage = func() {
		fmt.Fprintf(commandLine.Output(), `tgo is the function tracer for Go programs.

Usage:

  %s <command> [arguments]

Commands:

  launch   launches and traces a new process
  attach   attaches to the exisiting process

Use "tgo <command> --help" for more information about a command.
`, os.Args[0])
		commandLine.PrintDefaults()
	}

	if len(os.Args) < 2 {
		commandLine.Usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "launch":
		err = launchCmd(os.Args[2:])
	case "attach":
		err = attachCmd(os.Args[2:])
	case "server":
		err = serverCmd(os.Args[2:])
	default:
		commandLine.Usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
