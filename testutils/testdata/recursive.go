package main

import "fmt"

func dec(i, rem int) int {
	if rem == 0 {
		return i
	}
	return dec(i-1, rem-1)
}

func main() {
	val := dec(1, 100)
	fmt.Println(val)
}
