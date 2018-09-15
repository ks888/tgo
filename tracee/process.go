package tracee

import (
	"encoding/binary"
	"fmt"

	"github.com/ks888/tgo/debugapi/lldb"
)

var breakpointInsts = []byte{0xcc}

// Process represents the tracee process launched by or attached to this tracer.
type Process struct {
	debugapiClient  *lldb.Client
	currentThreadID int
	// knownGoRoutines is the list of go routines the tracer touched at least once, not the complete list.
	knownGoRoutines []goRoutine
	breakpoints     map[uint64]breakpoint
	Binary          Binary
}

type breakpoint struct {
	addr     uint64
	orgInsts []byte
	// targetGoRoutineID is go routine id interested in this breakpoint.
	targetGoRoutineID int
}

// LaunchProcess launches new tracee process.
func LaunchProcess(name string, arg ...string) (*Process, error) {
	debugapiClient := lldb.NewClient()
	threadID, err := debugapiClient.LaunchProcess(name, arg...)
	if err != nil {
		return nil, err
	}

	binary, err := NewBinary(name)
	if err != nil {
		return nil, err
	}

	proc := &Process{
		debugapiClient:  debugapiClient,
		currentThreadID: threadID,
		Binary:          binary,
		breakpoints:     make(map[uint64]breakpoint),
	}
	return proc, nil
}

// func AttachProcess(pid int) error

// SetBreakpoint sets the breakpoint at the specified address.
func (p *Process) SetBreakpoint(addr uint64) error {
	return p.SetConditionalBreakpoint(addr, 0)
}

// SetConditionalBreakpoint sets the conditional breakpoint.
// So far the only condition available is the go routine id.
func (p *Process) SetConditionalBreakpoint(addr uint64, goRoutineID int) error {
	bp, ok := p.breakpoints[addr]
	if ok {
		p.breakpoints[addr] = breakpoint{addr: bp.addr, orgInsts: bp.orgInsts, targetGoRoutineID: goRoutineID}
		return nil
	}

	originalInsts := make([]byte, len(breakpointInsts))
	if err := p.debugapiClient.ReadMemory(addr, originalInsts); err != nil {
		return err
	}

	if err := p.debugapiClient.WriteMemory(addr, breakpointInsts); err != nil {
		return err
	}

	p.breakpoints[addr] = breakpoint{addr: addr, orgInsts: originalInsts, targetGoRoutineID: goRoutineID}
	return nil
}

// ClearBreakpoint clears the breakpoint at the specified address.
func (p *Process) ClearBreakpoint(addr uint64) error {
	bp, ok := p.breakpoints[addr]
	if !ok {
		return nil
	}

	if err := p.debugapiClient.WriteMemory(addr, bp.orgInsts); err != nil {
		return err
	}

	delete(p.breakpoints, addr)
	return nil
}

// HitBreakpoint checks the current go routine meets the condition of the breakpoint.
func (p *Process) HitBreakpoint(addr uint64, goRoutineID int) bool {
	bp, ok := p.breakpoints[addr]
	if !ok {
		return false
	}

	return bp.targetGoRoutineID == 0 || bp.targetGoRoutineID == goRoutineID
}

// func (p *Process) currentGoRoutine() *goRoutine

func (p *Process) currentGoRoutineID() (int64, error) {
	// TODO: depend on go version and os
	var offset uint32 = 0x8a0
	gAddr, err := p.debugapiClient.ReadTLS(p.currentThreadID, offset)
	if err != nil {
		return 0, fmt.Errorf("failed to read tls: %v", err)
	}

	buff := make([]byte, 8)
	// TODO: use the 'runtime.g' type info in the dwarf info section.
	var offsetToID uint64 = 8*2 + 8 + 8 + 8 + 8 + 8 + 8*7 + 8 + 8 + 8 + 8 + 4 + 4
	if err = p.debugapiClient.ReadMemory(gAddr+offsetToID, buff); err != nil {
		return 0, fmt.Errorf("failed to read memory: %v", err)
	}

	return int64(binary.LittleEndian.Uint64(buff)), nil
}

type goRoutine struct {
	id int
	// currStackFrame is used to handle recursive func's case.
	currStackFrame uint64
	singleStepping bool
}
