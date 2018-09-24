package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/ks888/tgo/tracer"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s [tracee] [tracee args...]", os.Args[0])
		os.Exit(1)
	}

	controller := tracer.NewController()
	if err := controller.LaunchTracee(os.Args[1], os.Args[2:]...); err != nil {
		fmt.Printf("failed to launch tracee: %v", err)
		os.Exit(1)
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	go func() {
		_ = <-ch
		controller.Interrupt()
	}()

	if err := controller.MainLoop(); err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
}
