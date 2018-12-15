package main

import (
	"fmt"

	"github.com/ks888/tgo/lib/tracer"
)

//go:noinline
func tracedFunc() {
	fmt.Println("traced")
}

func main() {
	tracer.SetTraceLevel(2)
	if err := tracer.Start(); err != nil {
		panic(err)
	}

	// start again (should be no-op)
	if err := tracer.Start(); err != nil {
		panic(err)
	}

	tracedFunc()

	tracer.Stop()
	tracer.Stop() // stop again

	fmt.Println("not traced")

	// start again
	if err := tracer.Start(); err != nil {
		panic(err)
	}
}
