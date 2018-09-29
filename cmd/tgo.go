package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/ks888/tgo/tracer"
)

func run(pid int, function string, args []string) error {
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

	controller.SetTracingPoint(function)

	return controller.MainLoop()
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] [program] [args...]\n", os.Args[0])
		flag.PrintDefaults()
	}

	// TODO: offer subcommand for the attach case
	pid := flag.Int("attach", 0, "the `pid` to attach")
	function := flag.String("func", "main.main", "the tracing is enabled when this `function` is called and then disabled when returned")
	flag.Parse()
	if *pid == 0 && flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}
	args := flag.Args()

	if err := run(*pid, *function, args); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
