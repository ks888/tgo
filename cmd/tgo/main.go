package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ks888/tgo/log"
	"github.com/ks888/tgo/service"
)

const (
	traceOptionDesc      = "The tracing is enabled when this `function` is called and then disabled when returned."
	tracelevelOptionDesc = "Functions are traced if the stack depth is within this `tracelevel`. The stack depth here is based on the point the tracing is enabled."
	parselevelOptionDesc = "The trace log includes the function's args. The `parselevel` option determines how detailed these values should be."
	verboseOptionDesc    = "Show the debug-level message"
)

func serverCmd(args []string) error {
	commandLine := flag.NewFlagSet("", flag.ExitOnError)
	commandLine.Usage = func() {
		fmt.Fprintf(commandLine.Output(), `Usage:

  %s server [flags] [hostname:port]

Flags:
`, os.Args[0])
		commandLine.PrintDefaults()
	}
	verbose := commandLine.Bool("verbose", false, verboseOptionDesc)

	commandLine.Parse(args)
	if commandLine.NArg() < 1 {
		commandLine.Usage()
		os.Exit(1)
	}
	log.EnableDebugLog = *verbose

	return service.Serve(commandLine.Arg(0))
}

func main() {
	commandLine := flag.NewFlagSet("", flag.ExitOnError)
	commandLine.Usage = func() {
		fmt.Fprintf(commandLine.Output(), `tgo is the function tracer for Go programs.

Usage:

  %s <command> [arguments]

Commands:

  server   launches the server which offers tracing service. See https://godoc.org/github.com/ks888/tgo/service for the detail.

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
