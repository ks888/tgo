package main

import (
	"fmt"

	"github.com/ks888/tgo/lib/tracer"
)

func tracedFunc() {
	// do not change this order. Test the case in which the tracing point is same as the call-inst breakpoint.
	tracer.Start()
	f()
}

//go:noinline
func f() {
}

func main() {
	tracedFunc()

	fmt.Println("not traced")
}
