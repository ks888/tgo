# tgo: the function tracer for Go programs.

[![GoDoc](https://godoc.org/github.com/ks888/tgo?status.svg)](https://godoc.org/github.com/ks888/tgo/lib)
[![Build Status](https://travis-ci.com/ks888/tgo.svg?branch=master)](https://travis-ci.com/ks888/tgo)
[![Go Report Card](https://goreportcard.com/badge/github.com/ks888/tgo)](https://goreportcard.com/report/github.com/ks888/tgo)

### Example

```
% cat fib.go
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/ks888/tgo/lib/tracer"
)

func fib(n int) (r int) {
	if n == 0 || n == 1 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() {
	n, _ := strconv.ParseInt(os.Args[1], 10, 64)

	tracer.SetTraceLevel(3)
	tracer.Start()

	val := fib(int(n))
	fmt.Println(val)

	tracer.Stop()
}
% go build fib.go
% ./fib 3
\ (#01) main.fib(n = 3)
|\ (#01) main.fib(n = 2)
||\ (#01) main.fib(n = 1)
||/ (#01) main.fib() (r = 1)
||\ (#01) main.fib(n = 0)
||/ (#01) main.fib() (r = 0)
|/ (#01) main.fib() (r = 1)
|\ (#01) main.fib(n = 1)
|/ (#01) main.fib() (r = 1)
/ (#01) main.fib() (r = 2)
\ (#01) fmt.Println(a = -)
|\ (#01) fmt.Fprintln(a = -, w = -)
||\ (#01) fmt.newPrinter()
||/ (#01) fmt.newPrinter() (~r0 = -)
||\ (#01) fmt.(*pp).doPrintln(a = -, p = &{reordered: false, goodArgNum: true, panicking: false, erroring: false, buf: {...}, arg: nil, value: {...}, fmt: {...}})
||/ (#01) fmt.(*pp).doPrintln() ()
||\ (#01) os.(*File).Write(f = &{file: &{...}}, b = []{50, 10})
2
||/ (#01) os.(*File).Write() (n = 2, err = nil)
||\ (#01) fmt.(*pp).free(p = &{reordered: false, goodArgNum: true, panicking: false, erroring: false, buf: {...}, arg: int(2), value: {...}, fmt: {...}})
||/ (#01) fmt.(*pp).free() ()
|/ (#01) fmt.Fprintln() (n = 2, err = nil)
/ (#01) fmt.Println() (n = 2, err = nil)
```

### Install

*Note: for now, tgo only supports Mac OS X and go 1.10 or later.*

```
go get -u github.com/ks888/tgo/cmd/tgo
```

### Usage

Call `tracer.Start()` to start tracing and call `tracer.Stop()` to stop tracing. That's it!

There are some options which change how detailed the traced logs are and the output writer of these logs. See the [godoc]((https://godoc.org/github.com/ks888/tgo/lib)) for more info.
