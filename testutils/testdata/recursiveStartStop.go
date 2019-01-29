package main

import (
	"fmt"

	"github.com/ks888/tgo/lib/tracer"
)

func dec(i, rem int) int {
	tracer.Start()
	defer tracer.Stop()
	if rem == 0 {
		return i
	}
	return dec(i-1, rem-1)
}

func main() {
	tracer.SetTraceLevel(3)
	val := dec(3, 3)
	fmt.Println(val)
}
