package main

import "fmt"

func main() {
	f()
	fmt.Println("Returned normally from f.")
}

func f() {
	defer catch()
	fmt.Println("Calling g.")
	g(0)
	fmt.Println("Returned normally from g.")
}

func g(i int) {
	if i > 1 {
		throw(i)
	}
	defer through(i)
	fmt.Println("Printing in g", i)
	g(i + 1)
}

func throw(i int) {
	fmt.Println("Panicking!")
	panic(fmt.Sprintf("%v", i))
}

func through(i int) {
	// to check the case in which call 'defer' while handling panic.
	insideThrough := func() {
		fmt.Println("through", i)
	}
	defer insideThrough()
}

func catch() {
	if r := recover(); r != nil {
		fmt.Println("Recovered in f", r)
	}
}
