package main

import (
	"fmt"
	"os"

	"github.com/ks888/tgo/lib/tracer"
)

func tracedFunc() {
	if err := tracer.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
		return
	}

	fmt.Println("traced")
}

func main() {
	tracedFunc()

	fmt.Println("not traced")
}
