# tgo: a function tracer to boost your debugging

[![GoDoc](https://godoc.org/github.com/ks888/tgo?status.svg)](https://godoc.org/github.com/ks888/tgo/lib/tracer)
[![Build Status](https://travis-ci.com/ks888/tgo.svg?branch=master)](https://travis-ci.com/ks888/tgo)
[![Go Report Card](https://goreportcard.com/badge/github.com/ks888/tgo)](https://goreportcard.com/report/github.com/ks888/tgo)

### Examples

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
% go build fib.go
% ./fib 3
\ (#01) strconv.ParseInt(s = "3", base = 10, bitSize = 64) (...)
/ (#01) strconv.ParseInt(s = "3", base = 10, bitSize = 64) (i = 3, err = nil)
\ (#01) main.fib(n = 3) (...)
/ (#01) main.fib(n = 3) (~r1 = 2)
\ (#01) fmt.Println(a = []{int(2)}) (...)
2
/ (#01) fmt.Println(a = []{int(2)}) (n = 2, err = nil)
```

Now let's use the tgo to boost your debugging. The example below sorts the int slice using the quicksort, *but it has the bug*.

```golang
package main

import (
	"fmt"

	"github.com/ks888/tgo/lib/tracer"
)

func qsort(data []int, start, end int) {
	if end-start <= 1 {
		return
	}

	index := partition(data, start, end)
	qsort(data, start, index-1)
	qsort(data, index, end)
	return
}

func partition(data []int, start, end int) int {
	pivot := data[(start+end)/2]
	left, right := start, end
	for {
		for left <= end && data[left] < pivot {
			left++
		}
		for right >= start && data[right] > pivot {
			right--
		}
		if left > right {
			return left
		}

		data[left], data[right] = data[right], data[left]
		left++
		right--
	}
}

func main() {
	tracer.SetTraceLevel(2) // will be explained later
	tracer.Start()

	testdata := []int{3, 1, 2, 5, 4}
	qsort(testdata, 0, len(testdata)-1)

	tracer.Stop()

	fmt.Println(testdata)
}
```

The result is `[2 1 3 4 5]`, which is obviously not sorted.

```shell
% go build qsort.go
% ./qsort
\ (#01) main.qsort(data = []{3, 1, 2, 5, 4}, start = 0, end = 4) ()
|\ (#01) main.partition(data = []{3, 1, 2, 5, 4}, start = 0, end = 4) (...)
|/ (#01) main.partition(data = []{2, 1, 3, 5, 4}, start = 0, end = 4) (~r3 = 2)
|\ (#01) main.qsort(data = []{2, 1, 3, 5, 4}, start = 0, end = 1) ()
|/ (#01) main.qsort(data = []{2, 1, 3, 5, 4}, start = 0, end = 1) ()
|\ (#01) main.qsort(data = []{2, 1, 3, 5, 4}, start = 2, end = 4) ()
|/ (#01) main.qsort(data = []{2, 1, 3, 4, 5}, start = 2, end = 4) ()
/ (#01) main.qsort(data = []{2, 1, 3, 4, 5}, start = 0, end = 4) ()
[2 1 3 4 5]
```

Now the trace logs help you find a bug.

1. The 1st line of the logs shows `main.qsort` is called properly. `main.qsort` sorts the `data` between `start` and `end` (inclusive).
2. The 2nd and 3rd lines tell us `main.partition` works as expected here. `main.partition` partitions the `data` between `start` and `end` (inclusive) and returns the pivot index.
3. The 4th and 5th lines are ... strange. `start` is 0 and `end` is 1, but the `data` between 0 and 1 is not sorted.

Let's see the implementation of `main.qsort`, assuming `start` is 0 and `end` is 1. It becomes apparent the `if` statement at the 1st line has the off-by-one error. `end-start <= 1` should be `end-start <= 0`. We found the bug.

It's possible to use print debugging instead, but we may need to scatter print functions and run the code again and again. Also, it's likely the logs are less structured. The tgo approach is faster and clearer and so can boost your debugging.

### Features

* As easy as print debug: just insert 1-2 line(s) to your code
* But more powerful: visually tells you what's actually going on in your code
* Works without debugging info
  * It's actually important for testing. See the usage below for more details.
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
\ (#01) main.fib(n = 3) (...)
/ (#01) main.fib(n = 3) (~r1 = 2)
\ (#01) fmt.Println(a = []{int(2)}) (...)
2
/ (#01) fmt.Println(a = []{int(2)}) (n = 2, err = nil)
```

All the examples in this doc are available in the `_examples` directory. If this example doesn't work, check the error value `tracer.Start()` returns.

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
\ (#01) main.fib(n = 3) (...)
|\ (#01) main.fib(n = 2) (...)
|/ (#01) main.fib(n = 2) (~r1 = 1)
|\ (#01) main.fib(n = 1) (...)
|/ (#01) main.fib(n = 1) (~r1 = 1)
/ (#01) main.fib(n = 3) (~r1 = 2)
\ (#01) fmt.Println(a = []{int(2)}) (...)
|\ (#01) fmt.Fprintln(a = -, w = -) (...)
2
|/ (#01) fmt.Fprintln(a = -, w = -) (n = 2, err = nil)
/ (#01) fmt.Println(a = []{int(2)}) (n = 2, err = nil)
```

Note that the input args of `fmt.Fprintln` is `-` (not available) here. It's likely the debugging info are omitted due to optimization. To see the complete result, set `"-gcflags=-N"` (fast, but may not complete) or `"-gcflags=all=-N"` (slow, but complete) to `GOFLAGS` environment variable.

```shell
% GOFLAGS="-gcflags=all=-N" go build tracelevel.go
```

#### Works without debugging info

If you run the program with `go test` or `go run`, debugging info, such as DWARF data, are dropped. Fortunately, tgo works even in such a case. Let's trace the test for the fib function:

```golang
package main

import (
	"testing"

	"github.com/ks888/tgo/lib/tracer"
)

func TestFib(t *testing.T) {
	tracer.Start()
	actual := fib(3)
	tracer.Stop()

	if actual != 2 {
		t.Errorf("wrong: %v", actual)
	}
}
```

```
% go test -v fib.go fib_test.go
=== RUN   TestFib
\ (#06) command-line-arguments.fib(0x3, 0x0) ()
/ (#06) command-line-arguments.fib(0x3, 0x2) ()
--- PASS: TestFib (0.46s)
PASS
ok      command-line-arguments  0.485s
```

Functions are traced as usual, but args are the entire args value, divided by pointer size. In this example, `\ (#06) command-line-arguments.fib(0x3, 0x0) ()` indicates 1st input args is `0x3` and the initial return value is `0x0`. Because it's difficult to separate input args from the return args without debugging info, all args are shown as the input args. This format imitates the stack trace shown in the case of panic.

If you want to show the args as usual, set `"-ldflags=-w=false"` to `GOFLAGS` environment variable so that the debugging info is included in the binary. For example:

```
% GOFLAGS="-ldflags=-w=false" go test -v fib.go fib_test.go
=== RUN   TestFib
\ (#18) command-line-arguments.fib(n = 3) (...)
/ (#18) command-line-arguments.fib(n = 3) (~r1 = 2)
--- PASS: TestFib (0.55s)
PASS
ok      command-line-arguments  0.578s
```

Note that GOFLAGS is not supported in go 1.10 or earlier.

#### Tips

There are some random tips:
* There are more options to change the tgo's behaviors. See the [godoc](https://godoc.org/github.com/ks888/tgo/lib/tracer) for details.
* When a go routine calls `tracer.Start()`, it means only that go routine is traced. Other go routines are not affected. Similarly, `tracer.Stop()` just stops the tracing of the go routine which called the Stop function.
* Builtin functions are not traced. These functions are usually replaced with `runtime` package functions or assembly instructions.
