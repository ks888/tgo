package tracee

import (
	"debug/dwarf"
	"encoding/binary"
	"fmt"

	"github.com/ks888/tgo/debugapi"
	"github.com/ks888/tgo/debugapi/lldb"
)

var breakpointInsts = []byte{0xcc}

// Process represents the tracee process launched by or attached to this tracer.
type Process struct {
	debugapiClient *lldb.Client
	breakpoints    map[uint64]*breakpoint
	Binary         Binary
}

const countDisabled = -1

// StackFrame describes the data in the stack frame and its associated function.
type StackFrame struct {
	Function        *Function
	InputArguments  []Argument
	OutputArguments []Argument
	ReturnAddress   uint64
}

// Argument represents the value passed to the function.
type Argument struct {
	Name  string
	Typ   dwarf.Type
	Value []byte
}

// LaunchProcess launches new tracee process.
func LaunchProcess(name string, arg ...string) (*Process, error) {
	debugapiClient := lldb.NewClient()
	if err := debugapiClient.LaunchProcess(name, arg...); err != nil {
		return nil, err
	}

	binary, err := NewBinary(name)
	if err != nil {
		return nil, err
	}

	proc := &Process{
		debugapiClient: debugapiClient,
		breakpoints:    make(map[uint64]*breakpoint),
		Binary:         binary,
	}
	return proc, nil
}

// AttachProcess attaches to the existing tracee process.
func AttachProcess(pid int) (*Process, error) {
	debugapiClient := lldb.NewClient()
	err := debugapiClient.AttachProcess(pid)
	if err != nil {
		return nil, err
	}

	programPath, err := findProgramPath(pid)
	if err != nil {
		return nil, err
	}

	binary, err := NewBinary(programPath)
	if err != nil {
		return nil, err
	}

	proc := &Process{
		debugapiClient: debugapiClient,
		breakpoints:    make(map[uint64]*breakpoint),
		Binary:         binary,
	}
	return proc, nil
}

// Detach detaches from the tracee process. All breakpoints are cleared.
func (p *Process) Detach() error {
	for breakpointAddr := range p.breakpoints {
		if err := p.ClearBreakpoint(breakpointAddr); err != nil {
			return err
		}
	}

	if err := p.debugapiClient.DetachProcess(); err != nil {
		return err
	}

	return p.close()
}

func (p *Process) close() error {
	return p.Binary.Close()
}

// ContinueAndWait continues the execution and waits until an event happens.
// Note that the id of the stopped thread may be different from the id of the continued thread.
func (p *Process) ContinueAndWait() (trappedThreadIDs []int, event debugapi.Event, err error) {
	trappedThreadIDs, event, err = p.debugapiClient.ContinueAndWait()
	if debugapi.IsExitEvent(event.Type) {
		err = p.close()
	}
	return trappedThreadIDs, event, err
}

// SingleStep executes one instruction while clearing and setting breakpoints.
// It assumes there is the breakpoint at 'trappedAddr'.
func (p *Process) SingleStep(threadID int, trappedAddr uint64) error {
	bp, ok := p.breakpoints[trappedAddr]
	if !ok {
		return fmt.Errorf("breakpoint is not set at %x", trappedAddr)
	}

	if err := p.setPC(threadID, trappedAddr); err != nil {
		return err
	}

	if err := p.debugapiClient.WriteMemory(trappedAddr, bp.orgInsts); err != nil {
		return err
	}

	if _, _, err := p.stepAndWait(threadID); err != nil {
		return err
	}

	return p.debugapiClient.WriteMemory(trappedAddr, breakpointInsts)
}

func (p *Process) setPC(threadID int, addr uint64) error {
	regs, err := p.debugapiClient.ReadRegisters(threadID)
	if err != nil {
		return err
	}

	regs.Rip = addr
	return p.debugapiClient.WriteRegisters(threadID, regs)
}

func (p *Process) stepAndWait(threadID int) (trappedThreadIDs []int, event debugapi.Event, err error) {
	trappedThreadIDs, event, err = p.debugapiClient.StepAndWait(threadID)
	if debugapi.IsExitEvent(event.Type) {
		err = p.close()
	}
	return trappedThreadIDs, event, err
}

// SetBreakpoint sets the breakpoint at the specified address.
func (p *Process) SetBreakpoint(addr uint64) error {
	return p.SetConditionalBreakpoint(addr, 0)
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

// SetConditionalBreakpoint sets the breakpoint which only the specified go routine hits.
func (p *Process) SetConditionalBreakpoint(addr uint64, goRoutineID int64) error {
	bp, ok := p.breakpoints[addr]
	if ok {
		if goRoutineID != 0 {
			bp.AddTarget(goRoutineID)
		}
		return nil
	}

	originalInsts := make([]byte, len(breakpointInsts))
	if err := p.debugapiClient.ReadMemory(addr, originalInsts); err != nil {
		return err
	}

	if err := p.debugapiClient.WriteMemory(addr, breakpointInsts); err != nil {
		return err
	}

	bp = newBreakpoint(addr, originalInsts)
	if goRoutineID != 0 {
		bp.AddTarget(goRoutineID)
	}
	p.breakpoints[addr] = bp
	return nil
}

// ClearConditionalBreakpoint clears the conditional breakpoint at the specified address and go routine.
// The breakpoint may still exist on memory if there are other go routines not cleared.
// Use SingleStep() to temporary disable the breakpoint.
func (p *Process) ClearConditionalBreakpoint(addr uint64, goRoutineID int64) error {
	bp, ok := p.breakpoints[addr]
	if !ok {
		return nil
	}
	bp.RemoveTarget(goRoutineID)

	if !bp.NoTarget() {
		return nil
	}

	return p.ClearBreakpoint(addr)
}

// HitBreakpoint checks the current go routine meets the condition of the breakpoint.
func (p *Process) HitBreakpoint(addr uint64, goRoutineID int64) bool {
	bp, ok := p.breakpoints[addr]
	if !ok {
		return false
	}

	return bp.Hit(goRoutineID)
}

// HasBreakpoint returns true if the the breakpoint is already set at the specified address.
func (p *Process) HasBreakpoint(addr uint64) bool {
	_, ok := p.breakpoints[addr]
	return ok
}

// StackFrameAt returns the stack frame to which the given rbp specified.
// To get the correct stack frame, it assumes:
// * rbp+8 points to the return address.
// * rbp+16 points to the beginning of the args list.
func (p *Process) StackFrameAt(rbp, rip uint64) (*StackFrame, error) {
	function, err := p.Binary.FindFunction(rip)
	if err != nil {
		return nil, err
	}

	buff := make([]byte, 8)
	if err := p.debugapiClient.ReadMemory(rbp+8, buff); err != nil {
		return nil, err
	}
	retAddr := binary.LittleEndian.Uint64(buff)

	inputArgs, outputArgs, err := p.currentArgs(function.Parameters, rbp+16)
	if err != nil {
		return nil, err
	}

	return &StackFrame{
		Function:        function,
		ReturnAddress:   retAddr,
		InputArguments:  inputArgs,
		OutputArguments: outputArgs,
	}, nil
}

func (p *Process) currentArgs(params []Parameter, addrBeginningOfArgs uint64) (inputArgs []Argument, outputArgs []Argument, err error) {
	for _, param := range params {
		size := param.Typ.Size()
		buff := make([]byte, size)
		if err = p.debugapiClient.ReadMemory(addrBeginningOfArgs+uint64(param.Offset), buff); err != nil {
			return
		}

		arg := Argument{Name: param.Name, Typ: param.Typ, Value: buff}
		if param.IsOutput {
			outputArgs = append(outputArgs, arg)
		} else {
			inputArgs = append(inputArgs, arg)
		}
	}
	return
}

// GoRoutineInfo describes the various info of the go routine like pc.
type GoRoutineInfo struct {
	ID               int64
	UsedStackSize    uint64
	CurrentPC        uint64
	CurrentStackAddr uint64
	DeferedBy        *DeferedBy
}

// DeferedBy holds the function's context, such as used stack address, at the time of defer.
type DeferedBy struct {
	UsedStackSize uint64
	PC            uint64
}

// CurrentGoRoutineInfo returns the go routine info associated with the go routine which hits the breakpoint.
func (p *Process) CurrentGoRoutineInfo(threadID int) (GoRoutineInfo, error) {
	// TODO: depend on go version and os
	var offset uint32 = 0x8a0
	gAddr, err := p.debugapiClient.ReadTLS(threadID, offset)
	if err != nil {
		return GoRoutineInfo{}, fmt.Errorf("failed to read tls: %v", err)
	}

	buff := make([]byte, 8)
	// TODO: use the 'runtime.g' type info in the dwarf info section.
	var offsetToID uint64 = 8*2 + 8 + 8 + 8 + 8 + 8 + 8*7 + 8 + 8 + 8 + 8 + 4 + 4
	if err = p.debugapiClient.ReadMemory(gAddr+offsetToID, buff); err != nil {
		return GoRoutineInfo{}, fmt.Errorf("failed to read memory: %v", err)
	}
	id := int64(binary.LittleEndian.Uint64(buff))

	var offsetToStackHi uint64 = 8
	if err = p.debugapiClient.ReadMemory(gAddr+offsetToStackHi, buff); err != nil {
		return GoRoutineInfo{}, fmt.Errorf("failed to read memory: %v", err)
	}
	stackHi := binary.LittleEndian.Uint64(buff)

	regs, err := p.debugapiClient.ReadRegisters(threadID)
	if err != nil {
		return GoRoutineInfo{}, err
	}
	usedStackSize := stackHi - regs.Rsp

	var offsetToDefer uint64 = 8*2 + 8 + 8 + 8
	if err = p.debugapiClient.ReadMemory(gAddr+offsetToDefer, buff); err != nil {
		return GoRoutineInfo{}, err
	}
	pointerToDefer := binary.LittleEndian.Uint64(buff)

	var deferedBy *DeferedBy
	if pointerToDefer != 0 {
		var offsetToSP uint64 = 4 + 4
		if err = p.debugapiClient.ReadMemory(pointerToDefer+offsetToSP, buff); err != nil {
			return GoRoutineInfo{}, err
		}
		stackAddress := binary.LittleEndian.Uint64(buff)
		usedStackSizeAtDefer := stackHi - stackAddress

		var offsetToPC uint64 = 4 + 4 + 8
		if err = p.debugapiClient.ReadMemory(pointerToDefer+offsetToPC, buff); err != nil {
			return GoRoutineInfo{}, err
		}
		pc := binary.LittleEndian.Uint64(buff)

		deferedBy = &DeferedBy{UsedStackSize: usedStackSizeAtDefer, PC: pc}
	}

	return GoRoutineInfo{ID: id, UsedStackSize: usedStackSize, CurrentPC: regs.Rip, CurrentStackAddr: regs.Rsp, DeferedBy: deferedBy}, nil
}

type breakpoint struct {
	addr     uint64
	orgInsts []byte
	// targetGoRoutineIDs are go routine ids interested in this breakpoint.
	// Empty list implies all the go routines are target.
	targetGoRoutineIDs []int64
}

func newBreakpoint(addr uint64, orgInsts []byte) *breakpoint {
	return &breakpoint{addr: addr, orgInsts: orgInsts}
}

func (bp *breakpoint) AddTarget(goRoutineID int64) {
	bp.targetGoRoutineIDs = append(bp.targetGoRoutineIDs, goRoutineID)
	return
}

func (bp *breakpoint) RemoveTarget(goRoutineID int64) {
	for i, candidate := range bp.targetGoRoutineIDs {
		if candidate == goRoutineID {
			bp.targetGoRoutineIDs = append(bp.targetGoRoutineIDs[0:i], bp.targetGoRoutineIDs[i+1:len(bp.targetGoRoutineIDs)]...)
			return
		}
	}
	return
}

func (bp *breakpoint) NoTarget() bool {
	return len(bp.targetGoRoutineIDs) == 0
}

func (bp *breakpoint) Hit(goRoutineID int64) bool {
	for _, existingID := range bp.targetGoRoutineIDs {
		if existingID == goRoutineID {
			return true
		}
	}

	return len(bp.targetGoRoutineIDs) == 0
}
