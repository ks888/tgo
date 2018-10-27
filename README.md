# tgo: the function tracer for Go programs.

### Example

```
% cat fib.go
package main

import (
        "os"
        "strconv"
)

func fib(n int) (r int) {
        if n == 0 || n == 1 {
                return n
        }
        return fib(n-1) + fib(n-2)
}

func main() {
        n, _ := strconv.ParseInt(os.Args[1], 10, 64)
        print(fib(int(n)))
}
% go build fib.go
% tgo -trace main.main -tracelevel 3 launch ./fib 3
\ (#01) main.main()
|\ (#01) strconv.ParseInt(s = "3", base = 10, bitSize = 64)
||\ (#01) strconv.ParseUint(s = "3", base = 10, bitSize = 64)
||/ (#01) strconv.ParseUint() (~r3 = -, ~r4 = -)
|/ (#01) strconv.ParseInt() (i = 3, err = nil)
|\ (#01) main.fib(n = 3)
||\ (#01) main.fib(n = 2)
|||\ (#01) main.fib(n = 1)
|||/ (#01) main.fib() (r = 1)
|||\ (#01) main.fib(n = 0)
|||/ (#01) main.fib() (r = 0)
||/ (#01) main.fib() (r = 1)
||\ (#01) main.fib(n = 1)
||/ (#01) main.fib() (r = 1)
|/ (#01) main.fib() (r = 2)
|\ (#01) runtime.printlock()
|/ (#01) runtime.printlock() ()
|\ (#01) runtime.printint(v = 2)
||\ (#01) runtime.printuint(v = -)
|||\ (#01) runtime.printlock()
|||/ (#01) runtime.printlock() ()
|||\ (#01) runtime.memmove()
|||/ (#01) runtime.memmove() ()
|||\ (#01) runtime.printunlock()
|||/ (#01) runtime.printunlock() ()
|||\ (#01) gosave()
|||/ (#01) gosave() ()
2||/ (#01) runtime.printuint() ()
|/ (#01) runtime.printint() ()
|\ (#01) runtime.printunlock()
|/ (#01) runtime.printunlock() ()
/ (#01) main.main() ()
```

### Install

*Note: for now, tgo only supports Mac OS X and go 1.10 or later.*

```
go get -u github.com/ks888/tgo/cmd/tgo
```

### Usage

```
% tgo                                                                                                                                                    [master]
tgo is the function tracer for Go programs.

Usage:

        tgo [flags] <command> [command arguments]

Commands:

        launch   launches and traces a new process
        attach   attaches to the exisiting process

Flags:
  -parselevel parselevel
        The trace log includes the function's args. The parselevel option determines how detailed these values should be. (default 1)
  -trace function
        The tracing is enabled when this function is called and then disabled when returned. (default "main.main")
  -tracelevel tracelevel
        Functions are traced if the stack depth is within this tracelevel when the function is called. The stack depth here is based on the point the tracing is enabled. (default 1)
  -verbose
        Show the logging message
```
