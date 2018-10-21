package main

//go:noinline
func printBool(v bool) {
}

//go:noinline
func printInt8(v int8) {
}

//go:noinline
func printInt16(v int16) {
}

//go:noinline
func printInt32(v int32) {
}

//go:noinline
func printInt64(v int64) {
}

//go:noinline
func printUint8(v uint8) {
}

//go:noinline
func printUint16(v uint16) {
}

//go:noinline
func printUint32(v uint32) {
}

//go:noinline
func printUint64(v uint64) {
}

//go:noinline
func printFloat32(v float32) {
}

//go:noinline
func printFloat64(v float64) {
}

//go:noinline
func printComplex64(v complex64) {
}

//go:noinline
func printComplex128(v complex128) {
}

//go:noinline
func printString(v string) {
}

//go:noinline
func printArray(v [2]int) {
}

//go:noinline
func printSlice(v []int) {
}

type S struct {
	a    int
	b, c int
	T
}

func (s S) M() {
}

type T struct {
	d int
}

//go:noinline
func printStruct(v S) {
}

//go:noinline
func printPtr(v *int) {
}

//go:noinline
func printFunc(v func(int)) {
}

type I interface {
	M()
}

//go:noinline
func printInterface(v I) {
}

//go:noinline
func printNilInterface(v I) {
}

//go:noinline
func printEmptyInterface(v interface{}) {
}

//go:noinline
func printNilEmptyInterface(v interface{}) {
}

//go:noinline
func printMap(v map[int]int) {
}

//go:noinline
func printChan(v chan int) {
}

func main() {
	printBool(true)
	printInt8(-1)
	printInt16(-2)
	printInt32(-3)
	printInt64(-4)
	printUint8(^uint8(0))
	printUint16(^uint16(0))
	printUint32(^uint32(0))
	printUint64(^uint64(0))
	printFloat32(0.123456789)
	printFloat64(0.1234567890123456789)
	printComplex64(complex(1, 2))
	printComplex128(complex(3, 4))
	printString("hello\n")
	printArray([2]int{1, 2})
	printSlice([]int{3, 4})
	printStruct(S{a: 1, b: 2, c: 3, T: T{d: 4}})
	v := 1
	printPtr(&v)
	printFunc(func(v int) {})
	printInterface(S{a: 5, b: 6, c: 7, T: T{d: 8}})
	printNilInterface(nil)
	printEmptyInterface(S{a: 9})
	printNilEmptyInterface(nil)
	printMap(map[int]int{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9, 10: 10})
	printChan(make(chan int))
}
