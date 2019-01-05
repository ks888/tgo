package main

import (
	"reflect"
)

// reflect.DeepEqual calls runtime.duffzero, which directly jumps to the middle of the function.
func checkReflectDeepEqual(arr1, arr2 []int) bool {
	return reflect.DeepEqual(arr1, arr2)
}

func main() {
	checkReflectDeepEqual([]int{1}, []int{2})
}
