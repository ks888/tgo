package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/ks888/tgo/lib/tracer"
)

func fib(n int) int {
	if n == 0 || n == 1 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() {
	tracer.Start()

	var n int64
	if len(os.Args) > 1 {
		n, _ = strconv.ParseInt(os.Args[1], 10, 64)
	}
	val := fib(int(n))
	fmt.Println(val)

	tracer.Stop()
}
