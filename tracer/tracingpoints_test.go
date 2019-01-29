package tracer

import "testing"

func TestTracingGoRoutines_AddAndRemove(t *testing.T) {
	list := tracingGoRoutines{}
	var id int64 = 1
	list.Add(id)
	if !list.Tracing(id) {
		t.Errorf("go routine id %d is not traced", id)
	}

	list.Remove(1)
	if list.Tracing(id) {
		t.Errorf("go routine id %d is still traced", id)
	}
}

func TestTracingGoRoutines_MultipleAddAndRemove(t *testing.T) {
	list := tracingGoRoutines{}
	var id int64 = 1
	list.Add(id)
	if !list.Tracing(id) {
		t.Errorf("go routine id %d is not traced", id)
	}
	list.Add(id)

	list.Remove(1)
	if !list.Tracing(id) {
		t.Errorf("go routine id %d is not traced", id)
	}

	list.Remove(1)
	if list.Tracing(id) {
		t.Errorf("go routine id %d is still traced", id)
	}
}
