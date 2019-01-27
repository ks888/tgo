package tracer

import "github.com/ks888/tgo/log"

type tracingPoints struct {
	startAddressList []uint64
	endAddressList   []uint64
	goRoutinesInside []int64
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

// Enter updates the inside go routines list.
// If one go routine call this method N times, the go routine needs to call the Exit method N times to exit.
func (p *tracingPoints) Enter(goRoutineID int64) {
	if !p.Inside(goRoutineID) {
		log.Debugf("Start tracing of go routine #%d", goRoutineID)
	}

	p.goRoutinesInside = append(p.goRoutinesInside, goRoutineID)
	return
}

// Exit removes the go routine from the inside go routines list.
func (p *tracingPoints) Exit(goRoutineID int64) {
	for i, existingGoRoutine := range p.goRoutinesInside {
		if existingGoRoutine == goRoutineID {
			p.goRoutinesInside = append(p.goRoutinesInside[0:i], p.goRoutinesInside[i+1:]...)
			break
		}
	}

	if !p.Inside(goRoutineID) {
		log.Debugf("End tracing of go routine #%d", goRoutineID)
	}
	return
}

// Inside returns true if the go routine is inside the tracing point.
func (p *tracingPoints) Inside(goRoutineID int64) bool {
	for _, existingGoRoutine := range p.goRoutinesInside {
		if existingGoRoutine == goRoutineID {
			return true
		}
	}
	return false
}
