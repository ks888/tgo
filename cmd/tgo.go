package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/ks888/tgo/tracer"
)

func run() error {
	// TODO: use 3rd party flag management package
	pid := flag.Int("attach", 0, "pid to attach")
	flag.Parse()

	controller := tracer.NewController()
	if *pid == 0 {
		if err := controller.LaunchTracee(os.Args[1], os.Args[2:]...); err != nil {
			return fmt.Errorf("failed to launch tracee: %v", err)
		}
	} else {
		if err := controller.AttachTracee(*pid); err != nil {
			return fmt.Errorf("failed to attach tracee: %v", err)
		}
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	go func() {
		_ = <-ch
		controller.Interrupt()
	}()

	return controller.MainLoop()
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf(`Usage: %s [tracee] [tracee args...]
       %s -attach [pid]
`, os.Args[0], os.Args[0])
		os.Exit(1)
	}

	if err := run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
