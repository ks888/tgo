# tgo: a function tracer to boost your debugging

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

### Features

* As easy as print debug: just insert 1-2 line(s) to your code
* The verbosity level of the trace logs is customizable
* Works with optimized binary
  * though logs may lack some information. See the usage below for more details.
* Support Linux/Mac OS X

### Install

*Note: supported go version is 1.10 or later.*

#### Mac OS X

Install the `tgo` binary and its library:

```
go get -u github.com/ks888/tgo/cmd/tgo
```

If you have an error, you may need to install Xcode Command Line Tools: `xcode-select --install`

#### Linux

Install the `tgo` binary and its library:

```
go get -u github.com/ks888/tgo/cmd/tgo
```

The `tgo` binary attaches to your process using the ptrace mechanism. To enable this, run the command below:

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

Basically you just need to specify the starting point with `tracer.Start()` and the ending point with `tracer.Stop()`.

```golang
package main

import (
	"fmt"

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

	val := fib(3)
	fmt.Println(val)

	tracer.Stop()
}
```

When you build and run this program, you can see the function trace logs of `fib()` and `fmt.Println()`. Though `fib()` is called recursively, only the 1st call's log is printed.

```shell
% go build simple.go
% ./simple
\ (#01) main.fib(n = 3)
/ (#01) main.fib() (~r1 = 2)
\ (#01) fmt.Println(a = []{int(2)})
2
/ (#01) fmt.Println() (n = 2, err = nil)
```

If this example doesn't work, check the error value `tracer.Start()` returns.

In this example, you may omit the the `tracer.Stop()` line because tracing automatically ends when the caller function of `tracer.Start()` returns.

#### Set TraceLevel

You often need a little deeper trace logs. For example, you may want to know the functions `fib()` and `fmt.Println()` call internally. You can do that by adjusting the trace level with `tracer.SetTraceLevel()`.

```golang
package main

import (
	"fmt"

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

	val := fib(3)
	fmt.Println(val)

	tracer.Stop()
}
```

In this example, the trace level is 2. Because the default level is 1, now the trace logs go one level deeper.

```shell
% go build tracelevel.go
% ./tracelevel
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

Note that the input args of `fmt.Fprintln` is `-` (not available) here. It's likely the debugging info are omitted due to optimization. To see the complete result, set `"-gcflags=-N"` (fast, but may not complete) or `"-gcflags=all=-N"` (slow, but complete) to `GOFLAGS` environment variable.

#### Works with optimized binary

As the last example above shows, the binary is optimized by default. Further, if you run the program with `go run` or `go test`, some debugging info, such as DWARF sections, are dropped. 

Fortunately, tgo works even in such a case. Let's run the simple example above using `go run`:

```
% go run simple.go
\ (#01) main.fib(0x3, 0x0)
/ (#01) main.fib() (0x3, 0x2)
\ (#01) fmt.Println(0xc0000d1f78, 0x1, 0x1, 0xc00013c368, 0x0, 0x0)
2
/ (#01) fmt.Println() (0xc0000d1f78, 0x1, 0x1, 0x2, 0x0, 0x0)
```

Functions are traced as usual, but its args part is the entire args value in the stack frame, shown by pointer size. In this example, `\ (#01) main.fib(0x3, 0x0)` means the 1st 8 byte of args value is 0x3, which is the input args of the `main.fib()`. Also, the next 8 byte of args value is 0x0, which is the initial value of the return value. Because it's difficult to separate input args from the return args, all the args are shown in any case. Actually, this format is same as the stack trace shown in the case of panic.

If you want to show the args as usual, set `"-ldflags=-w=false"` to `GOFLAGS` environment variable so that the debugging info is included in the binary. For example:

```
% GOFLAGS="-ldflags=-w=false" go run simple.go
\ (#01) main.fib()
/ (#01) main.fib() ()
\ (#01) fmt.Println(a = []{int(2)})
2
/ (#01) fmt.Println() (n = 2, err = nil)
```

Note that GOFLAGS is not supported in go 1.10 or earlier. Also, some debugging info may be still insufficient especially in `go run` case. Consider using `go build` in that case.

#### Tips

There are some random tips:
* There are more options to change the tgo's behaviors. See the [godoc](https://godoc.org/github.com/ks888/tgo/lib/tracer) for details.
* When a go routine calls `tracer.Start()`, it means only the go routine is traced and other go routines are not.
* Builtin functions are not traced. These functions are usually replaced with `runtime` package functions or assembly instructions.
