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
