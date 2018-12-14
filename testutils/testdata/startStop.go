package main

import (
	"fmt"
	"os"

	"github.com/ks888/tgo/lib/tracer"
)

//go:noinline
func tracedFunc() {
	fmt.Println("traced")
}

func main() {
	tracer.SetTraceLevel(2)
	if err := tracer.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
		return
	}

	tracedFunc()

	tracer.Stop()

	fmt.Println("not traced")
}
