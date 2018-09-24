package main

import (
	"fmt"
	"math/rand"
)

//go:noinline
func noParameter() {
	fmt.Println("Hello world")
}

//go:noinline
func oneParameter(a int) {
	fmt.Println(a)
}

//go:noinline
func oneParameterAndOneVariable(i int) {
	a := rand.Int()
	fmt.Println(i, a)
	fmt.Println(i, a)
}

//go:noinline
func twoParameters(j, i int) { // intentionally inverse
	fmt.Println(i, j)
}

func main() {
	noParameter()
	oneParameter(1)
	oneParameterAndOneVariable(1)
	twoParameters(1, 1)
}
