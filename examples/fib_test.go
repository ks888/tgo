package main

import (
	"testing"

	"github.com/ks888/tgo/lib/tracer"
)

func TestFib(t *testing.T) {
	tracer.Start()
	actual := fib(3)
	tracer.Stop()

	if actual != 2 {
		t.Errorf("wrong: %v", actual)
	}
}
