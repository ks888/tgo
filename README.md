# tgo: the function tracer for Go programs.

[![GoDoc](https://godoc.org/github.com/ks888/tgo?status.svg)](https://godoc.org/github.com/ks888/tgo/lib/tracer)
[![Build Status](https://travis-ci.com/ks888/tgo.svg?branch=master)](https://travis-ci.com/ks888/tgo)
[![Go Report Card](https://goreportcard.com/badge/github.com/ks888/tgo)](https://goreportcard.com/report/github.com/ks888/tgo)

### Example

In this example, the functions called between `tracer.Start()` and `tracer.Stop()` are traced.

```golang
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
	tracer.SetTraceLevel(2)
	tracer.Start()

	var n int64
	if len(os.Args) > 1 {
		n, _ = strconv.ParseInt(os.Args[1], 10, 64)
	}
	val := fib(int(n))
	fmt.Println(val)

	tracer.Stop()
}
```

When you run the program, the trace logs are printed:

```shell
% go build fibonacci.go
% ./fibonacci 3
\ (#01) strconv.ParseInt(s = "3", base = 10, bitSize = 64)
|\ (#01) strconv.ParseUint(s = "3", base = 10, bitSize = 64)
|/ (#01) strconv.ParseUint() (~r3 = 3, ~r4 = nil)
/ (#01) strconv.ParseInt() (i = 3, err = nil)
\ (#01) main.fib(n = 3)
|\ (#01) main.fib(n = 2)
|/ (#01) main.fib() (~r1 = 1)
|\ (#01) main.fib(n = 1)
|/ (#01) main.fib() (~r1 = 1)
/ (#01) main.fib() (~r1 = 2)
\ (#01) fmt.Println(a = []{int(2)})
|\ (#01) fmt.Fprintln(a = -, w = -)
2
|/ (#01) fmt.Fprintln() (n = 2, err = nil)
/ (#01) fmt.Println() (n = 2, err = nil)
```

### Install

*Note: supported go version is 1.10 or later.*

#### Mac OS X

Install the `tgo` binary and its library:

```
go get -u github.com/ks888/tgo/cmd/tgo
```

#### Linux

Install the `tgo` binary and its library:

```
go get -u github.com/ks888/tgo/cmd/tgo
```

tgo depends on the ptrace mechanism and attaches to the non-descendant process. For this, run the command below:

```
sudo sh -c 'echo 0 > /proc/sys/kernel/yama/ptrace_scope'
```

If you run the program in the docker container, add the `--cap-add sys_ptrace` option. For example:

```
docker run --cap-add sys_ptrace -it golang:1 /bin/bash
```

#### Windows

Not supported.

### Usage

Call `tracer.Start()` to start tracing and call `tracer.Stop()` (or just return from the caller of `tracer.Start()`) to stop tracing. That's it!

There are some options which change how detailed the traced logs are and the output writer of these logs. See the [godoc](https://godoc.org/github.com/ks888/tgo/lib/tracer) for more info.
