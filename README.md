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

tgo depends on the ptrace mechanism and attaches to the non-descendant process. To enable this, run the command below:

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

When you build and run this program, you can see the function trace logs of the 1st call of `fib()` and `fmt.Println()`. Though `fib()` is called recursively, only the 1st call's log is printed.

```shell
% go build simple.go
% ./simple
\ (#01) main.fib(n = 3)
/ (#01) main.fib() (~r1 = 2)
\ (#01) fmt.Println(a = []{int(2)})
2
/ (#01) fmt.Println() (n = 2, err = nil)
```

If this example doesn't work, try to print the error value `tracer.Start()` returns.

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

In this example, the trace level is 2. Now the functions (1st call of) `fib()` and `fmt.Println()` calls internally are traced.

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

#### Works with optimized binary

Some debugging info, such as DWARF sections, are dropped if you run the program with `go run` or `go test`. Fortunately tgo works even in such a case, though the args part of trace logs are not perfect.

Let's run the simple example above using `go run`:

```
% go run simple.go
\ (#01) main.fib(0x3, 0x0)
/ (#01) main.fib() (0x3, 0x2)
\ (#01) fmt.Println(0xc0000d1f78, 0x1, 0x1, 0xc00013c368, 0x0, 0x0)
2
/ (#01) fmt.Println() (0xc0000d1f78, 0x1, 0x1, 0x2, 0x0, 0x0)
```

The call of / return from functions are traced as usual, but its args parts show the entire value of the args in the stack frame by pointer size. In this example, `\ (#01) main.fib(0x3, 0x0)` means the 1st 8 byte of args value is 0x3, which is the 1st input args of the `main.fib()`. Also, `/ (#01) main.fib() (0x3, 0x2)` means the 2nd 8 byte of args value is 0x2, which is the 1st return value. Actually, it's same format as the stack trace shown in the case of panic.

If you want to show the args as usual, set `"-ldflags=-w=false"` to `GOFLAGS` environment variable so that the debugging info is included in the binary. For example:

```
% GOFLAGS="-ldflags=-w=false" go run simple.go
\ (#01) main.fib()
/ (#01) main.fib() ()
\ (#01) fmt.Println(a = []{int(2)})
2
/ (#01) fmt.Println() (n = 2, err = nil)
```

Note that GOFLAGS is not supported in go 1.10 or earlier. Also, some debugging info may be still insufficient (especially in `go run` case). Consider using `go build` or `go test -o` in that case.

#### Tips

There are some random tips:
* The output writer of trace logs can be changed. See the [godoc](https://godoc.org/github.com/ks888/tgo/lib/tracer) for more info.
* When a go routine calls `tracer.Start()`, it means just that go routine is traced and other go routines are not.
* Builtin functions are not traced. These functions are usually `runtime` functions at runtime and it's difficult to determine which `runtime` functions correspond to which builtin functions.
* If the value part of the args is `-`, for example `fmt.Fprintln(a = -, w = -)`, it's likely the debugging info are omitted due to optimization. To see the complete result, set `"-gcflags=all=-N -gcflags=-l"` to `GOFLAGS` environment variable.
