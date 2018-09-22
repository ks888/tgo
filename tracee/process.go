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
	debugapiClient  *lldb.Client
	currentThreadID int
	breakpoints     map[uint64]breakpoint
	Binary          Binary
}

type breakpoint struct {
	addr     uint64
	orgInsts []byte
	// targetGoRoutineID is go routine id interested in this breakpoint.
	targetGoRoutineID int
}

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

// GoRoutineInfo describes the various info of the go routine like stack frame.
type GoRoutineInfo struct {
	ID            int64
	UsedStackSize uint64
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
		breakpoints:     make(map[uint64]breakpoint),
		Binary:          binary,
	}
	return proc, nil
}

// TODO: support it. Need to get the program name from the pid
// AttachProcess attaches to the existing tracee process.
// func AttachProcess(pid int) (*Process, error) {
// 	debugapiClient := lldb.NewClient()
// 	threadID, err := debugapiClient.AttachProcess(pid)
// 	if err != nil {
// 		return nil, err
// 	}

// 	binary, err := NewBinary(name)
// 	if err != nil {
// 		return nil, err
// 	}

// 	proc := &Process{
// 		debugapiClient:  debugapiClient,
// 		currentThreadID: threadID,
// 		breakpoints:     make(map[uint64]breakpoint),
// 		Binary:          binary,
// 	}
// 	return proc, nil
// }

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
func (p *Process) ContinueAndWait() (event debugapi.Event, err error) {
	p.currentThreadID, event, err = p.debugapiClient.ContinueAndWait()
	if debugapi.IsExitEvent(event.Type) {
		err = p.close()
	}
	return event, err
}

// StepAndWait does the single-step execution.
func (p *Process) StepAndWait() (event debugapi.Event, err error) {
	p.currentThreadID, event, err = p.debugapiClient.StepAndWait(p.currentThreadID)
	if debugapi.IsExitEvent(event.Type) {
		err = p.close()
	}
	return event, err
}

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

// HasBreakpoint returns true if the the breakpoint is already set at the specified address.
func (p *Process) HasBreakpoint(addr uint64) bool {
	_, ok := p.breakpoints[addr]
	return ok
}

// SetPC set the pc of the current thread to the given address.
func (p *Process) SetPC(addr uint64) error {
	regs, err := p.debugapiClient.ReadRegisters(p.currentThreadID)
	if err != nil {
		return err
	}

	regs.Rip = addr
	return p.debugapiClient.WriteRegisters(p.currentThreadID, regs)
}

// CurrentStackFrame returns the stack frame of the go routine associated with the stopped thread.
func (p *Process) CurrentStackFrame() (*StackFrame, error) {
	regs, err := p.debugapiClient.ReadRegisters(p.currentThreadID)
	if err != nil {
		return nil, err
	}

	function, err := p.Binary.FindFunction(regs.Rip)
	if err != nil {
		return nil, err
	}

	buff := make([]byte, 8)
	// assumes the rsp points to the return address in the beginning of the function.
	if err := p.debugapiClient.ReadMemory(regs.Rsp, buff); err != nil {
		return nil, err
	}
	retAddr := binary.LittleEndian.Uint64(buff)

	// assumes the rsp+8 points to the beginning of the args.
	inputArgs, outputArgs, err := p.currentArgs(function.Parameters, regs.Rsp+8)
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

// CurrentGoRoutineInfo returns the go routine info associated with the go routine which hits the breakpoint.
func (p *Process) CurrentGoRoutineInfo() (GoRoutineInfo, error) {
	// TODO: depend on go version and os
	var offset uint32 = 0x8a0
	gAddr, err := p.debugapiClient.ReadTLS(p.currentThreadID, offset)
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

	regs, err := p.debugapiClient.ReadRegisters(p.currentThreadID)
	if err != nil {
		return GoRoutineInfo{}, err
	}
	usedStackSize := stackHi - regs.Rsp

	return GoRoutineInfo{ID: id, UsedStackSize: usedStackSize}, nil
}
