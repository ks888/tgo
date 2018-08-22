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

func main() {
	noParameter()
	oneParameter(1)
	oneParameterAndOneVariable(1)
}
