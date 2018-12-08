package main

import (
	"fmt"
	"os"

	"github.com/ks888/tgo/lib/tracer"
)

func main() {
	if err := tracer.On(tracer.NewDefaultOption()); err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
		return
	}

	fmt.Println("traced")

	if err := tracer.Off(); err != nil {
		fmt.Fprintf(os.Stderr, "%v", err)
	}
}
