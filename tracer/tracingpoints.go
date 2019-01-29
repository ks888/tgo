package tracer

import "github.com/ks888/tgo/log"

type tracingPoints struct {
	startAddressList []uint64
	endAddressList   []uint64
}

// IsStartAddress returns true if the addr is same as the start address.
func (p *tracingPoints) IsStartAddress(addr uint64) bool {
	for _, startAddr := range p.startAddressList {
		if startAddr == addr {
			return true
		}
	}
	return false
}

// IsEndAddress returns true if the addr is same as the end address.
func (p *tracingPoints) IsEndAddress(addr uint64) bool {
	for _, endAddr := range p.endAddressList {
		if endAddr == addr {
			return true
		}
	}
	return false
}

type tracingGoRoutines []int64

// Add adds the go routine to the tracing list.
// If one go routine call this method N times, the go routine needs to call the Remove method N times to exit.
func (t *tracingGoRoutines) Add(goRoutineID int64) {
	if !t.Tracing(goRoutineID) {
		log.Debugf("Start tracing of go routine #%d", goRoutineID)
	}

	*t = append(*t, goRoutineID)
	return
}

// Remove removes the go routine from the tracing list.
func (t *tracingGoRoutines) Remove(goRoutineID int64) {
	for i, existingGoRoutine := range *t {
		if existingGoRoutine == goRoutineID {
			*t = append((*t)[0:i], (*t)[i+1:]...)
			break
		}
	}

	if !t.Tracing(goRoutineID) {
		log.Debugf("End tracing of go routine #%d", goRoutineID)
	}
	return
}

// Tracing returns true if the go routine is traced.
func (t *tracingGoRoutines) Tracing(goRoutineID int64) bool {
	for _, existingGoRoutine := range *t {
		if existingGoRoutine == goRoutineID {
			return true
		}
	}
	return false
}
