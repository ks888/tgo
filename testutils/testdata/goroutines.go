package main

import (
	"fmt"
	"time"
)

//go:noinline
func receive(ch chan int) int {
	return <-ch
}

//go:noinline
func send(ch chan int, val int) {
	ch <- val
}

func inc(input, output chan int) {
	val := receive(input)
	send(output, val+1)
}

func main() {
	chans := []chan int{make(chan int)}
	for i := 0; i < 20; i++ {
		fromCh := chans[len(chans)-1]
		var toCh = make(chan int)
		go inc(fromCh, toCh)

		chans = append(chans, toCh)
	}

	chans[0] <- 0
	val := <-chans[len(chans)-1]
	fmt.Println(val)

	// the main go routine may exit before all go routines created above exit and tracing ends.
	time.Sleep(100 * time.Millisecond)
}
