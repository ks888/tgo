package tracee

import (
	"debug/dwarf"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ks888/tgo/debugapi"
	"github.com/ks888/tgo/debugapi/lldb"
)

var breakpointInsts = []byte{0xcc}

// Process represents the tracee process launched by or attached to this tracer.
type Process struct {
	debugapiClient *lldb.Client
	breakpoints    map[uint64]*breakpoint
	Binary         Binary
	moduleDataList []moduleData
}

type moduleData struct {
	types, etypes uint64
}

const countDisabled = -1

// StackFrame describes the data in the stack frame and its associated function.
type StackFrame struct {
	Function        *Function
	InputArguments  []Argument
	OutputArguments []Argument
	ReturnAddress   uint64
}

// LaunchProcess launches new tracee process.
func LaunchProcess(name string, arg ...string) (*Process, error) {
	debugapiClient := lldb.NewClient()
	if err := debugapiClient.LaunchProcess(name, arg...); err != nil {
		return nil, err
	}

	return newProcess(debugapiClient, name)
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

	return newProcess(debugapiClient, programPath)
}

func newProcess(debugapiClient *lldb.Client, programPath string) (*Process, error) {
	binary, err := NewBinary(programPath)
	if err != nil {
		return nil, err
	}

	firstModuleDataAddr, err := binary.findFirstModuleDataAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to find firstmoduledata: %v", err)
	}

	moduleDataList, err := buildModuleDataList(firstModuleDataAddr, debugapiClient)
	if err != nil {
		return nil, err
	}

	return &Process{
		debugapiClient: debugapiClient,
		breakpoints:    make(map[uint64]*breakpoint),
		Binary:         binary,
		moduleDataList: moduleDataList,
	}, nil
}

func buildModuleDataList(firstModuleDataAddr uint64, debugapiClient *lldb.Client) ([]moduleData, error) {
	var moduleDataList []moduleData
	buff := make([]byte, 8)
	moduleDataAddr := firstModuleDataAddr
	for {
		// TODO: use the DIE of the moduleData type
		var offsetToTypes uint64 = 24*3 + 8*16
		if err := debugapiClient.ReadMemory(moduleDataAddr+offsetToTypes, buff); err != nil {
			return nil, err
		}
		types := binary.LittleEndian.Uint64(buff)

		var offsetToEtypes uint64 = 24*3 + 8*17
		if err := debugapiClient.ReadMemory(moduleDataAddr+offsetToEtypes, buff); err != nil {
			return nil, err
		}
		etypes := binary.LittleEndian.Uint64(buff)

		moduleDataList = append(moduleDataList, moduleData{types: types, etypes: etypes})

		var offsetToNext uint64 = 24*3 + 8*18 + 24*4 + 16 + 24 + 16 + 24 + 8 + (4+8)*2 + 8 + 1
		if err := debugapiClient.ReadMemory(moduleDataAddr+offsetToNext, buff); err != nil {
			return nil, err
		}
		next := binary.LittleEndian.Uint64(buff)
		if next == 0 {
			return moduleDataList, nil
		}

		moduleDataAddr = next
	}
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
func (p *Process) SingleStep(threadID int, trappedAddr uint64) error {
	if err := p.setPC(threadID, trappedAddr); err != nil {
		return err
	}

	bp, bpSet := p.breakpoints[trappedAddr]
	if bpSet {
		if err := p.debugapiClient.WriteMemory(trappedAddr, bp.orgInsts); err != nil {
			return err
		}
	}

	if _, err := p.stepAndWait(threadID); err != nil {
		unspecifiedError, ok := err.(debugapi.UnspecifiedThreadError)
		if !ok {
			return err
		}

		if err := p.singleStepUnspecifiedThreads(threadID, unspecifiedError); err != nil {
			return err
		}
		return p.SingleStep(threadID, trappedAddr)
	}

	if bpSet {
		return p.debugapiClient.WriteMemory(trappedAddr, breakpointInsts)
	}
	return nil
}

func (p *Process) setPC(threadID int, addr uint64) error {
	regs, err := p.debugapiClient.ReadRegisters(threadID)
	if err != nil {
		return err
	}

	regs.Rip = addr
	return p.debugapiClient.WriteRegisters(threadID, regs)
}

func (p *Process) stepAndWait(threadID int) (event debugapi.Event, err error) {
	event, err = p.debugapiClient.StepAndWait(threadID)
	if debugapi.IsExitEvent(event.Type) {
		err = p.close()
	}
	return event, err
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

func (p *Process) findImplType(interfaceTyp *dwarf.StructType, value []byte) (dwarf.Type, error) {
	return nil, nil
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

		arg := Argument{Name: param.Name, Typ: param.Typ, Value: buff, process: p}
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
	Panicking        bool
	PanicHandler     *PanicHandler
}

// PanicHandler holds the function info which (will) handles panic.
type PanicHandler struct {
	// UsedStackSizeAtDefer and PCAtDefer are the function info which register this handler by 'defer'.
	UsedStackSizeAtDefer uint64
	PCAtDefer            uint64
}

// CurrentGoRoutineInfo returns the go routine info associated with the go routine which hits the breakpoint.
func (p *Process) CurrentGoRoutineInfo(threadID int) (GoRoutineInfo, error) {
	// TODO: depend on go version and os
	var offset uint32 = 0x8a0
	gAddr, err := p.debugapiClient.ReadTLS(threadID, offset)
	if err != nil {
		unspecifiedError, ok := err.(debugapi.UnspecifiedThreadError)
		if !ok {
			return GoRoutineInfo{}, err
		}

		if err := p.singleStepUnspecifiedThreads(threadID, unspecifiedError); err != nil {
			return GoRoutineInfo{}, err
		}
		return p.CurrentGoRoutineInfo(threadID)
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

	var offsetToPanic uint64 = 8*2 + 8 + 8
	if err = p.debugapiClient.ReadMemory(gAddr+offsetToPanic, buff); err != nil {
		return GoRoutineInfo{}, err
	}
	panicAddr := binary.LittleEndian.Uint64(buff)
	panicking := panicAddr != 0

	panicHandler, err := p.findPanicHandler(gAddr, panicAddr, stackHi)
	if err != nil {
		return GoRoutineInfo{}, err
	}

	return GoRoutineInfo{ID: id, UsedStackSize: usedStackSize, CurrentPC: regs.Rip, CurrentStackAddr: regs.Rsp, Panicking: panicking, PanicHandler: panicHandler}, nil
}

func (p *Process) singleStepUnspecifiedThreads(threadID int, err debugapi.UnspecifiedThreadError) error {
	for _, unspecifiedThread := range err.ThreadIDs {
		if unspecifiedThread == threadID {
			continue
		}

		regs, err := p.debugapiClient.ReadRegisters(threadID)
		if err != nil {
			return err
		}
		if err := p.SingleStep(unspecifiedThread, regs.Rip-1); err != nil {
			return err
		}
	}
	return nil
}

func (p *Process) findPanicHandler(gAddr, panicAddr, stackHi uint64) (*PanicHandler, error) {
	buff := make([]byte, 8)
	var offsetToDefer uint64 = 8*2 + 8 + 8 + 8
	if err := p.debugapiClient.ReadMemory(gAddr+offsetToDefer, buff); err != nil {
		return nil, err
	}
	pointerToDefer := binary.LittleEndian.Uint64(buff)

	for pointerToDefer != 0 {
		var offsetToPanicInDefer uint64 = 4 + 4 + 8 + 8 + 8
		if err := p.debugapiClient.ReadMemory(pointerToDefer+offsetToPanicInDefer, buff); err != nil {
			return nil, err
		}
		panicInDefer := binary.LittleEndian.Uint64(buff)
		if panicInDefer == panicAddr {
			break
		}

		var offsetToNextDefer uint64 = 4 + 4 + 8 + 8 + 8 + 8
		if err := p.debugapiClient.ReadMemory(pointerToDefer+offsetToNextDefer, buff); err != nil {
			return nil, err
		}
		pointerToDefer = binary.LittleEndian.Uint64(buff)
	}

	if pointerToDefer == 0 {
		return nil, nil
	}

	var offsetToSP uint64 = 4 + 4
	if err := p.debugapiClient.ReadMemory(pointerToDefer+offsetToSP, buff); err != nil {
		return nil, err
	}
	stackAddress := binary.LittleEndian.Uint64(buff)
	usedStackSizeAtDefer := stackHi - stackAddress

	var offsetToPC uint64 = 4 + 4 + 8
	if err := p.debugapiClient.ReadMemory(pointerToDefer+offsetToPC, buff); err != nil {
		return nil, err
	}
	pc := binary.LittleEndian.Uint64(buff)

	return &PanicHandler{UsedStackSizeAtDefer: usedStackSizeAtDefer, PCAtDefer: pc}, nil
}

// ThreadInfo describes the various info of thread.
type ThreadInfo struct {
	ID               int
	CurrentPC        uint64
	CurrentStackAddr uint64
}

// CurrentThreadInfo returns the thread info of the specified thread ID.
func (p *Process) CurrentThreadInfo(threadID int) (ThreadInfo, error) {
	regs, err := p.debugapiClient.ReadRegisters(threadID)
	if err != nil {
		return ThreadInfo{}, err
	}
	return ThreadInfo{ID: threadID, CurrentPC: regs.Rip, CurrentStackAddr: regs.Rsp}, nil
}

// Argument represents the value passed to the function.
type Argument struct {
	Name    string
	Typ     dwarf.Type
	Value   []byte
	process *Process
}

func (arg Argument) String() string {
	return fmt.Sprintf("%s = %s", arg.Name, parseValue(arg.Typ, arg.Value, arg.process.debugapiClient))
}

// TODO: define memory reader interface
func parseValue(rawTyp dwarf.Type, value []byte, debugapiClient *lldb.Client) string {
	switch typ := rawTyp.(type) {
	case *dwarf.IntType:
		switch typ.Size() {
		case 1:
			return fmt.Sprintf("%d", int8(value[0]))
		case 2:
			return fmt.Sprintf("%d", int16(binary.LittleEndian.Uint16(value)))
		case 4:
			return fmt.Sprintf("%d", int32(binary.LittleEndian.Uint32(value)))
		case 8:
			return fmt.Sprintf("%d", int64(binary.LittleEndian.Uint64(value)))
		}
	case *dwarf.UintType:
		switch typ.Size() {
		case 1:
			return fmt.Sprintf("%d", value[0])
		case 2:
			return fmt.Sprintf("%d", binary.LittleEndian.Uint16(value))
		case 4:
			return fmt.Sprintf("%d", binary.LittleEndian.Uint32(value))
		case 8:
			return fmt.Sprintf("%d", binary.LittleEndian.Uint64(value))
		}
	case *dwarf.BoolType:
		if value[0] == 0 {
			return "false"
		} else if value[0] == 1 {
			return "true"
		}
	case *dwarf.FloatType:
		switch typ.Size() {
		case 4:
			raw := binary.LittleEndian.Uint32(value)
			return fmt.Sprintf("%g", math.Float32frombits(raw))
		case 8:
			raw := binary.LittleEndian.Uint64(value)
			return fmt.Sprintf("%g", math.Float64frombits(raw))
		}
	case *dwarf.ComplexType:
		switch typ.Size() {
		case 8:
			real := math.Float32frombits(binary.LittleEndian.Uint32(value[0:4]))
			img := math.Float32frombits(binary.LittleEndian.Uint32(value[4:8]))
			return fmt.Sprintf("%g", complex(real, img))
		case 16:
			real := math.Float64frombits(binary.LittleEndian.Uint64(value[0:8]))
			img := math.Float64frombits(binary.LittleEndian.Uint64(value[8:16]))
			return fmt.Sprintf("%g", complex(real, img))
		}
	case *dwarf.PtrType:
		addr := binary.LittleEndian.Uint64(value)
		buff := make([]byte, typ.Type.Size())
		if err := debugapiClient.ReadMemory(addr, buff); err != nil {
			break
		}
		return fmt.Sprintf("&%s", parseValue(typ.Type, buff, debugapiClient))
	case *dwarf.FuncType:
		addr := binary.LittleEndian.Uint64(value)
		return fmt.Sprintf("%#x", addr)
	case *dwarf.StructType:
		switch {
		case typ.StructName == "string":
			addr := binary.LittleEndian.Uint64(value[:8])
			len := int(binary.LittleEndian.Uint64(value[8:]))
			buff := make([]byte, len)
			if err := debugapiClient.ReadMemory(addr, buff); err != nil {
				break
			}
			return strconv.Quote(string(buff))
		case strings.HasPrefix(typ.StructName, "[]"):
			return parseSliceValue(typ, value, debugapiClient)
		case typ.StructName == "runtime.iface":
			return parseInterfaceValue(typ, value, debugapiClient)
		default:
			return parseStructValue(typ, value, debugapiClient)
		}
	case *dwarf.ArrayType:
		if typ.Count == -1 {
			break
		}
		var vals []string
		stride := int(typ.Type.Size()) // StrideBitSize is 0
		for i := 0; i < int(typ.Count); i++ {
			vals = append(vals, parseValue(typ.Type, value[i*stride:(i+1)*stride], debugapiClient))
		}
		return fmt.Sprintf("[%d]{%s}", typ.Count, strings.Join(vals, ", "))
	case *dwarf.TypedefType:
		return parseValue(typ.Type, value, debugapiClient)
	}
	return fmt.Sprintf("%v", value)
}

func parseSliceValue(typ *dwarf.StructType, value []byte, debugapiClient *lldb.Client) string {
	var length int
	var elemType dwarf.Type
	var pointer uint64
	for _, field := range typ.Field {
		if field.Name == "len" {
			length = int(binary.LittleEndian.Uint64(value[field.ByteOffset : field.ByteOffset+field.Type.Size()]))
		} else if field.Name == "array" {
			pointerType, ok := field.Type.(*dwarf.PtrType)
			if !ok {
				return ""
			}
			elemType = pointerType.Type
			pointer = binary.LittleEndian.Uint64(value[field.ByteOffset : field.ByteOffset+field.Type.Size()])
		}
	}

	buff := make([]byte, int(elemType.Size())*length)
	if err := debugapiClient.ReadMemory(pointer, buff); err != nil {
		return ""
	}

	vals := make([]string, 0, length)
	for i := 0; i < length; i++ {
		val := parseValue(elemType, buff[int(elemType.Size())*i:int(elemType.Size())*(i+1)], debugapiClient)
		vals = append(vals, val)
	}
	return fmt.Sprintf("[]{%s}", strings.Join(vals, ", "))
}

func parseInterfaceValue(typ *dwarf.StructType, value []byte, debugapiClient *lldb.Client) string {
	ptrToItabTyp, ptrToItab := memberOfStruct(typ, "tab", value, debugapiClient)
	if ptrToItabTyp == nil {
		return ""
	}

	itabTyp, itabVal := dereference(ptrToItabTyp, ptrToItab, debugapiClient)
	if itabTyp == nil {
		return ""
	}

	itabTypeTyp, itabTypeAddr := memberOfStruct(itabTyp, "_type", itabVal, debugapiClient)
	if itabTypeTyp == nil {
		return ""
	}

	return fmt.Sprintf("%#x{}", itabTypeAddr)
}

func parseStructValue(typ *dwarf.StructType, value []byte, debugapiClient *lldb.Client) string {
	var vals []string
	for _, field := range typ.Field {
		val := parseValue(field.Type, value[field.ByteOffset:field.ByteOffset+field.Type.Size()], debugapiClient)
		vals = append(vals, fmt.Sprintf("%s: %s", field.Name, val))
	}
	return fmt.Sprintf("{%s}", strings.Join(vals, ", "))
}

// memberOfStruct returns 'nil, nil' if not struct type or no such a member.
func memberOfStruct(typ dwarf.Type, memberName string, value []byte, debugapiClient *lldb.Client) (dwarf.Type, []byte) {
	structTyp, ok := typ.(*dwarf.StructType)
	if !ok {
		return nil, nil
	}

	for _, field := range structTyp.Field {
		if field.Name != memberName {
			continue
		}

		return field.Type, value[field.ByteOffset : field.ByteOffset+field.Type.Size()]
	}
	return nil, nil
}

func dereference(typ dwarf.Type, value []byte, debugapiClient *lldb.Client) (dwarf.Type, []byte) {
	var derefTyp dwarf.Type
	ptrTyp, ok := typ.(*dwarf.PtrType)
	if !ok {
		return typ, value
	}
	derefTyp = ptrTyp.Type

	if typedefTyp, ok := derefTyp.(*dwarf.TypedefType); ok {
		derefTyp = typedefTyp.Type
	}

	addr := binary.LittleEndian.Uint64(value)
	val := make([]byte, derefTyp.Size())
	if err := debugapiClient.ReadMemory(addr, val); err != nil {
		return nil, nil
	}
	return derefTyp, val
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
