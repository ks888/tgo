package tracer

import "testing"

func TestBreakpoints_SetHitAndClear(t *testing.T) {
	numSet, numCleared := 0, 0
	setBreakpoint := func(uint64) error { numSet++; return nil }
	clearBreakpoint := func(uint64) error { numCleared++; return nil }
	bps := NewBreakpoints(setBreakpoint, clearBreakpoint)

	if err := bps.Set(0x100); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if !bps.Hit(0x100, 1) {
		t.Errorf("not hit")
	}

	if err := bps.Clear(0x100); err != nil {
		t.Fatalf("failed to clear breakpoint: %v", err)
	}

	if numSet != 1 {
		t.Errorf("wrong number of set ops: %d", numSet)
	}
	if numCleared != 1 {
		t.Errorf("wrong number of clear ops: %d", numCleared)
	}
}

func TestBreakpoints_SetHitAndClearConditional(t *testing.T) {
	numSet, numCleared := 0, 0
	setBreakpoint := func(uint64) error { numSet++; return nil }
	clearBreakpoint := func(uint64) error { numCleared++; return nil }
	bps := NewBreakpoints(setBreakpoint, clearBreakpoint)

	if err := bps.SetConditional(0x100, 1); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if !bps.Hit(0x100, 1) {
		t.Errorf("not hit")
	}

	if err := bps.ClearConditional(0x100, 1); err != nil {
		t.Fatalf("failed to clear breakpoint: %v", err)
	}

	if numSet != 1 {
		t.Errorf("wrong number of set ops: %d", numSet)
	}
	if numCleared != 1 {
		t.Errorf("wrong number of clear ops: %d", numCleared)
	}
}

func TestBreakpoints_Set_SetConditionalBefore(t *testing.T) {
	setBreakpoint := func(uint64) error { return nil }
	clearBreakpoint := func(uint64) error { return nil }
	bps := NewBreakpoints(setBreakpoint, clearBreakpoint)

	if err := bps.SetConditional(0x100, 1); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if err := bps.Set(0x100); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if !bps.Hit(0x100, 2) {
		t.Errorf("previous conditions are not removed")
	}
}

func TestBreakpoints_SetConditional_SetBefore(t *testing.T) {
	setBreakpoint := func(uint64) error { return nil }
	clearBreakpoint := func(uint64) error { return nil }
	bps := NewBreakpoints(setBreakpoint, clearBreakpoint)

	if err := bps.Set(0x100); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if err := bps.SetConditional(0x100, 1); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if !bps.Hit(0x100, 2) {
		t.Errorf("SetConditional should be no-op here")
	}
}

func TestBreakpoints_Hit_NotSet(t *testing.T) {
	setBreakpoint := func(uint64) error { return nil }
	clearBreakpoint := func(uint64) error { return nil }
	bps := NewBreakpoints(setBreakpoint, clearBreakpoint)

	if bps.Hit(0x100, 1) {
		t.Errorf("should not hit")
	}
}

func TestBreakpoints_Hit_NotMeetCondition(t *testing.T) {
	setBreakpoint := func(uint64) error { return nil }
	clearBreakpoint := func(uint64) error { return nil }
	bps := NewBreakpoints(setBreakpoint, clearBreakpoint)

	if err := bps.SetConditional(0x100, 1); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if bps.Hit(0x100, 2) {
		t.Errorf("should not hit")
	}
}

func TestBreakpoints_Clear_ClearConditionals(t *testing.T) {
	setBreakpoint := func(uint64) error { return nil }
	clearBreakpoint := func(uint64) error { return nil }
	bps := NewBreakpoints(setBreakpoint, clearBreakpoint)

	if err := bps.SetConditional(0x100, 1); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if err := bps.Clear(0x100); err != nil {
		t.Fatalf("failed to clear breakpoint: %v", err)
	}

	if bps.Hit(0x100, 1) {
		t.Errorf("should not hit")
	}
}

func TestBreakpoints_ClearConditional_OtherCondtionsRemain(t *testing.T) {
	numCleared := 0
	setBreakpoint := func(uint64) error { return nil }
	clearBreakpoint := func(uint64) error { numCleared++; return nil }
	bps := NewBreakpoints(setBreakpoint, clearBreakpoint)

	if err := bps.SetConditional(0x100, 1); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if err := bps.SetConditional(0x100, 2); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if err := bps.ClearConditional(0x100, 1); err != nil {
		t.Fatalf("failed to clear breakpoint: %v", err)
	}

	if numCleared != 0 {
		t.Errorf("wrong number of clear ops: %d", numCleared)
	}
}

func TestBreakpoints_ClearAllByGoRoutineID(t *testing.T) {
	numCleared := 0
	setBreakpoint := func(uint64) error { return nil }
	clearBreakpoint := func(uint64) error { numCleared++; return nil }
	bps := NewBreakpoints(setBreakpoint, clearBreakpoint)

	if err := bps.SetConditional(0x100, 1); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if err := bps.SetConditional(0x200, 1); err != nil {
		t.Fatalf("failed to set breakpoint: %v", err)
	}

	if err := bps.ClearAllByGoRoutineID(1); err != nil {
		t.Fatalf("failed to clear breakpoint: %v", err)
	}

	if numCleared != 2 {
		t.Errorf("wrong number of clear ops: %d", numCleared)
	}
}
