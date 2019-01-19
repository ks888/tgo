package tracer

import "testing"

func TestTracingPoints_EnterAndExit(t *testing.T) {
	points := tracingPoints{}
	var id int64 = 1
	points.Enter(id)
	if !points.Inside(id) {
		t.Errorf("go routine id %d is not traced", id)
	}

	points.Exit(1)
	if points.Inside(id) {
		t.Errorf("go routine id %d is still traced", id)
	}
}
