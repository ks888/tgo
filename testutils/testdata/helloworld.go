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
func oneParameter(s []int) []int {
	s2 := []int{2}
	return append(s, s2...)
}

//go:noinline
func oneParameterAndOneVariable(i int) {
	a := rand.Int()
	fmt.Println(i, a)
	fmt.Println(i, a)
}

//go:noinline
func twoParameters(j, i int) { // intentionally inverse
	a := rand.Int()
	fmt.Println(j, a)
	fmt.Println(i, a)
}

//go:noinline
func twoReturns() (int, int) {
	return rand.Int(), rand.Int()
}

func main() {
	noParameter()
	oneParameter([]int{1})
	oneParameterAndOneVariable(1)
	twoParameters(1, 1)
	_, _ = twoReturns()
}
