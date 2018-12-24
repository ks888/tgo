package tracee

import (
	"debug/dwarf"
	"encoding/binary"
	"fmt"
	"runtime"

	"github.com/ks888/tgo/debugapi"
	"github.com/ks888/tgo/debugapi/lldb"
	"github.com/ks888/tgo/log"
)

var breakpointInsts = []byte{0xcc}

// Process represents the tracee process launched by or attached to this tracer.
type Process struct {
	debugapiClient *lldb.Client
	breakpoints    map[uint64]*breakpoint
	Binary         BinaryFile
	GoVersion      GoVersion
	moduleDataList []*moduleData
	valueParser    valueParser
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

	proc, err := newProcess(debugapiClient, name, runtime.Version())
	if err != nil {
		debugapiClient.DetachProcess()
	}
	return proc, err
}

// AttachProcess attaches to the existing tracee process.
func AttachProcess(pid int, programPath, goVersion string) (*Process, error) {
	debugapiClient := lldb.NewClient()
	err := debugapiClient.AttachProcess(pid)
	if err != nil {
		return nil, err
	}

	proc, err := newProcess(debugapiClient, programPath, goVersion)
	if err != nil {
		debugapiClient.DetachProcess() // keep the attached process running
	}
	return proc, err
}

func newProcess(debugapiClient *lldb.Client, programPath, rawGoVersion string) (*Process, error) {
	proc := &Process{debugapiClient: debugapiClient, breakpoints: make(map[uint64]*breakpoint)}

	proc.GoVersion = ParseGoVersion(rawGoVersion)
	var err error
	proc.Binary, err = OpenBinaryFile(programPath, proc.GoVersion)
	if err != nil {
		return nil, err
	}
	proc.moduleDataList = parseModuleDataList(proc.Binary, debugapiClient)
	proc.valueParser = valueParser{reader: debugapiClient, mapRuntimeType: proc.mapRuntimeType}
	return proc, nil
}

func parseModuleDataList(binary BinaryFile, reader memoryReader) (moduleDataList []*moduleData) {
	moduleDataAddr := binary.firstModuleDataAddress()
	for moduleDataAddr != 0 {
		md := newModuleData(moduleDataAddr, binary.moduleDataType())
		moduleDataList = append(moduleDataList, md)

		moduleDataAddr = md.next(reader)
	}
	return
}

// moduleData represents the value of the moduledata type.
// It offers a set of methods to get the field value of the type rather than simply returns the parsed result.
// It is because the moduledata can be large and the parsing cost is too high.
// TODO: try to use the parser by optimizing the load array operation.
type moduleData struct {
	moduleDataAddr uint64
	moduleDataType dwarf.Type
	fields         map[string]*dwarf.StructField
}

func newModuleData(moduleDataAddr uint64, moduleDataType dwarf.Type) *moduleData {
	fields := make(map[string]*dwarf.StructField)
	for _, field := range moduleDataType.(*dwarf.StructType).Field {
		fields[field.Name] = field
	}

	return &moduleData{moduleDataAddr: moduleDataAddr, moduleDataType: moduleDataType, fields: fields}
}

// pclntable retrieves the pclntable data specified by `index` because retrieving all the ftab data can be heavy.
func (md *moduleData) pclntable(reader memoryReader, index int) uint64 {
	ptrToArrayType, ptrToArray := md.retrieveArrayInSlice(reader, "pclntable")
	elementType := ptrToArrayType.(*dwarf.PtrType).Type

	return ptrToArray + uint64(index)*uint64(elementType.Size())
}

// functab retrieves the functab data specified by `index` because retrieving all the ftab data can be heavy.
func (md *moduleData) functab(reader memoryReader, index int) (entry, funcoff uint64) {
	ptrToFtabType, ptrToArray := md.retrieveArrayInSlice(reader, "ftab")
	ftabType := ptrToFtabType.(*dwarf.PtrType).Type
	functabSize := uint64(ftabType.Size())

	buff := make([]byte, functabSize)
	if err := reader.ReadMemory(ptrToArray+uint64(index)*functabSize, buff); err != nil {
		log.Debugf("failed to read memory: %v", err)
		return
	}

	for _, field := range ftabType.(*dwarf.StructType).Field {
		val := binary.LittleEndian.Uint64(buff[field.ByteOffset : field.ByteOffset+field.Type.Size()])
		switch field.Name {
		case "entry":
			entry = val
		case "funcoff":
			funcoff = val
		}
	}
	return
}

func (md *moduleData) ftabLen(reader memoryReader) int {
	return md.retrieveSliceLen(reader, "ftab")
}

func (md *moduleData) findfunctab(reader memoryReader) uint64 {
	return md.retrieveUint64(reader, "findfunctab")
}

func (md *moduleData) minpc(reader memoryReader) uint64 {
	return md.retrieveUint64(reader, "minpc")
}

func (md *moduleData) maxpc(reader memoryReader) uint64 {
	return md.retrieveUint64(reader, "maxpc")
}

func (md *moduleData) types(reader memoryReader) uint64 {
	return md.retrieveUint64(reader, "types")
}

func (md *moduleData) etypes(reader memoryReader) uint64 {
	return md.retrieveUint64(reader, "etypes")
}

func (md *moduleData) next(reader memoryReader) uint64 {
	return md.retrieveUint64(reader, "next")
}

func (md *moduleData) retrieveArrayInSlice(reader memoryReader, fieldName string) (dwarf.Type, uint64) {
	typ, buff := md.retrieveFieldOfStruct(reader, md.fields[fieldName], "array")
	if buff == nil {
		return nil, 0
	}

	return typ, binary.LittleEndian.Uint64(buff)
}

func (md *moduleData) retrieveSliceLen(reader memoryReader, fieldName string) int {
	_, buff := md.retrieveFieldOfStruct(reader, md.fields[fieldName], "len")
	if buff == nil {
		return 0
	}

	return int(binary.LittleEndian.Uint64(buff))
}

func (md *moduleData) retrieveFieldOfStruct(reader memoryReader, strct *dwarf.StructField, fieldName string) (dwarf.Type, []byte) {
	strctType, ok := strct.Type.(*dwarf.StructType)
	if !ok {
		log.Printf("unexpected type: %#v", md.fields[fieldName].Type)
		return nil, nil
	}

	var field *dwarf.StructField
	for _, candidate := range strctType.Field {
		if candidate.Name == fieldName {
			field = candidate
			break
		}
	}
	if field == nil {
		panic(fmt.Sprintf("%s field not found", fieldName))
	}

	buff := make([]byte, field.Type.Size())
	addr := md.moduleDataAddr + uint64(strct.ByteOffset) + uint64(field.ByteOffset)
	if err := reader.ReadMemory(addr, buff); err != nil {
		log.Debugf("failed to read memory: %v", err)
		return nil, nil
	}
	return field.Type, buff
}

func (md *moduleData) retrieveUint64(reader memoryReader, fieldName string) uint64 {
	field := md.fields[fieldName]
	if field.Type.Size() != 8 {
		log.Printf("the type size is not expected value: %d", field.Type.Size())
	}

	buff := make([]byte, 8)
	if err := reader.ReadMemory(md.moduleDataAddr+uint64(field.ByteOffset), buff); err != nil {
		log.Debugf("failed to read memory: %v", err)
		return 0
	}
	return binary.LittleEndian.Uint64(buff)
}

func (p *Process) mapRuntimeType(runtimeTypeAddr uint64) (dwarf.Type, error) {
	var md *moduleData
	var reader memoryReader = p.debugapiClient
	for _, candidate := range p.moduleDataList {
		if candidate.types(reader) <= runtimeTypeAddr && runtimeTypeAddr < candidate.etypes(reader) {
			md = candidate
			break
		}
	}

	return p.Binary.findDwarfTypeByAddr(runtimeTypeAddr - md.types(reader))
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
func (p *Process) ContinueAndWait() (debugapi.Event, error) {
	event, err := p.debugapiClient.ContinueAndWait()
	if debugapi.IsExitEvent(event.Type) {
		err = p.close()
	}
	return event, err
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

// StackFrameAt returns the stack frame to which the given rbp specified.
// To get the correct stack frame, it assumes:
// * rsp points to the return address.
// * rsp+8 points to the beginning of the args list.
//
// To be accurate, we need to check the .debug_frame section to find the CFA and return address.
// But we omit the check here because this function is called at only the beginning or end of the tracee's function call.
func (p *Process) StackFrameAt(rsp, rip uint64) (*StackFrame, error) {
	function, err := p.FindFunction(rip)
	if err != nil {
		return nil, err
	}

	buff := make([]byte, 8)
	if err := p.debugapiClient.ReadMemory(rsp, buff); err != nil {
		return nil, err
	}
	retAddr := binary.LittleEndian.Uint64(buff)

	inputArgs, outputArgs, err := p.currentArgs(function.Parameters, rsp+8)
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

// FindFunction finds the function to which pc specifies.
func (p *Process) FindFunction(pc uint64) (*Function, error) {
	function, err := p.Binary.FindFunction(pc)
	if err == nil {
		return function, err
	}

	return p.findFunctionByModuleData(pc)
}

const (
	// must be same as the values defined in runtime package
	numSubbuckets      = 16
	minfunc            = 16
	pcbucketsize       = 256 * minfunc
	sizeFindfuncbucket = 20
)

// Assume this dwarf.Type represents a subset of the _func type in the case DWARF is not available.
// TODO: check the assumption in CI.
var _funcType = &dwarf.StructType{
	StructName: "runtime._func",
	CommonType: dwarf.CommonType{ByteSize: 40},
	Field: []*dwarf.StructField{
		&dwarf.StructField{
			Name:       "entry",
			Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
			ByteOffset: 0,
		},
		&dwarf.StructField{
			Name:       "nameoff",
			Type:       &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 4}}},
			ByteOffset: 8,
		},
		&dwarf.StructField{
			Name:       "args",
			Type:       &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 4}}},
			ByteOffset: 12,
		},
	},
}

// findFunctionByModuleData has the same logic as the runtime.findfunc.
func (p *Process) findFunctionByModuleData(pc uint64) (*Function, error) {
	md := p.findModuleDataByPC(pc)
	if md == nil {
		return nil, fmt.Errorf("no moduledata found for pc %#x", pc)
	}

	buff, err := p.findFuncType(md, pc)
	if err != nil {
		return nil, err
	}

	var entry uint64
	var nameoff int32
	// var args int32
	for _, field := range _funcType.Field {
		rawData := buff[field.ByteOffset : field.ByteOffset+field.Type.Size()]
		switch field.Name {
		case "entry":
			entry = binary.LittleEndian.Uint64(rawData)
		case "nameoff":
			nameoff = int32(binary.LittleEndian.Uint32(rawData))
			// case "args":
			// 	args = int32(binary.LittleEndian.Uint32(rawData))
		}
	}

	ptrToFuncname := md.pclntable(p.debugapiClient, int(nameoff))
	var rawFuncname []byte
	for {
		buff := make([]byte, 16)
		if err := p.debugapiClient.ReadMemory(ptrToFuncname, buff); err != nil {
			return nil, err
		}

		for i := 0; i < len(buff); i++ {
			if buff[i] == 0 {
				rawFuncname = append(rawFuncname, buff[0:i]...)
				goto FoundFuncname
			}
		}

		rawFuncname = append(rawFuncname, buff...)
		ptrToFuncname += uint64(len(buff))
	}
FoundFuncname:
	funcname := string(rawFuncname)

	// TODO: set Parameters using args.

	return &Function{Name: funcname, Value: entry}, nil
}

func (p *Process) findModuleDataByPC(pc uint64) *moduleData {
	for _, moduleData := range p.moduleDataList {
		if moduleData.minpc(p.debugapiClient) <= pc && pc < moduleData.maxpc(p.debugapiClient) {
			return moduleData
		}
	}
	return nil
}

func (p *Process) findFuncType(md *moduleData, pc uint64) ([]byte, error) {
	x := pc - md.minpc(p.debugapiClient)
	bucketIndex := x / pcbucketsize
	subbucketIndex := x % pcbucketsize / (pcbucketsize / numSubbuckets)

	ptrToFindFuncBucket := md.findfunctab(p.debugapiClient) + bucketIndex*sizeFindfuncbucket
	buff := make([]byte, 4)
	if err := p.debugapiClient.ReadMemory(ptrToFindFuncBucket, buff); err != nil {
		return nil, err
	}
	var ftabIdx int = int(binary.LittleEndian.Uint32(buff))

	buff = make([]byte, 1)
	if err := p.debugapiClient.ReadMemory(ptrToFindFuncBucket+4+subbucketIndex, buff); err != nil {
		return nil, err
	}
	ftabIdx += int(buff[0])

	ftabLen := md.ftabLen(p.debugapiClient)
	if ftabIdx >= ftabLen {
		ftabIdx = ftabLen - 1
	}

	entry, funcoff := md.functab(p.debugapiClient, ftabIdx)
	if pc < entry {
		for entry > pc && ftabIdx > 0 {
			ftabIdx--
			entry, funcoff = md.functab(p.debugapiClient, ftabIdx)
		}
		if ftabIdx == 0 {
			panic("bad findfunctab entry idx")
		}
	} else {
		// linear search to find func with pc >= entry.
		nextEntry, nextFuncoff := md.functab(p.debugapiClient, ftabIdx+1)
		for nextEntry <= pc {
			entry = nextEntry
			funcoff = nextFuncoff
			ftabIdx++
			nextEntry, nextFuncoff = md.functab(p.debugapiClient, ftabIdx+1)
		}
	}

	funcTypePtr := md.pclntable(p.debugapiClient, int(funcoff))
	buff = make([]byte, _funcType.Size())
	if err := p.debugapiClient.ReadMemory(funcTypePtr, buff); err != nil {
		return nil, err
	}

	return buff, nil
}

func (p *Process) currentArgs(params []Parameter, addrBeginningOfArgs uint64) (inputArgs []Argument, outputArgs []Argument, err error) {
	for _, param := range params {
		param := param // without this, all the closures point to the last param.
		parseValue := func(depth int) value {
			if !param.Exist {
				return nil
			}

			size := param.Typ.Size()
			buff := make([]byte, size)
			if err = p.debugapiClient.ReadMemory(addrBeginningOfArgs+uint64(param.Offset), buff); err != nil {
				log.Debugf("failed to read the '%s' value: %v", param.Name, err)
				return nil
			}
			return p.valueParser.parseValue(param.Typ, buff, depth)
		}

		arg := Argument{Name: param.Name, Typ: param.Typ, parseValue: parseValue}
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
	Ancestors        []int64
}

// PanicHandler holds the function info which (will) handles panic.
type PanicHandler struct {
	// UsedStackSizeAtDefer and PCAtDefer are the function info which register this handler by 'defer'.
	UsedStackSizeAtDefer uint64
	PCAtDefer            uint64
}

// CurrentGoRoutineInfo returns the go routine info associated with the go routine which hits the breakpoint.
func (p *Process) CurrentGoRoutineInfo(threadID int) (GoRoutineInfo, error) {
	gAddr, err := p.debugapiClient.ReadTLS(threadID, p.offsetToG())
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

	_, idRawVal, err := p.findFieldInStruct(gAddr, p.Binary.runtimeGType(), "goid")
	if err != nil {
		return GoRoutineInfo{}, err
	}
	id := int64(binary.LittleEndian.Uint64(idRawVal))

	stackType, stackRawVal, err := p.findFieldInStruct(gAddr, p.Binary.runtimeGType(), "stack")
	if err != nil {
		return GoRoutineInfo{}, err
	}
	stackVal := p.valueParser.parseValue(stackType, stackRawVal, 1)
	stackHi := stackVal.(structValue).fields["hi"].(uint64Value).val

	regs, err := p.debugapiClient.ReadRegisters(threadID)
	if err != nil {
		return GoRoutineInfo{}, err
	}
	usedStackSize := stackHi - regs.Rsp

	_, panicRawVal, err := p.findFieldInStruct(gAddr, p.Binary.runtimeGType(), "_panic")
	if err != nil {
		return GoRoutineInfo{}, err
	}
	panicAddr := binary.LittleEndian.Uint64(panicRawVal)
	panicking := panicAddr != 0

	panicHandler, err := p.findPanicHandler(gAddr, panicAddr, stackHi)
	if err != nil {
		return GoRoutineInfo{}, err
	}

	ancestors, err := p.findAncestorGoIDs(gAddr)
	if err != nil {
		return GoRoutineInfo{}, err
	}

	return GoRoutineInfo{ID: id, UsedStackSize: usedStackSize, CurrentPC: regs.Rip, CurrentStackAddr: regs.Rsp, Panicking: panicking, PanicHandler: panicHandler, Ancestors: ancestors}, nil
}

// TODO: depend on os
func (p *Process) offsetToG() uint32 {
	if p.GoVersion.LaterThan(GoVersion{MajorVersion: 1, MinorVersion: 11}) {
		return 0x30
	}
	return 0x8a0
}

func (p *Process) singleStepUnspecifiedThreads(threadID int, err debugapi.UnspecifiedThreadError) error {
	for _, unspecifiedThread := range err.ThreadIDs {
		if unspecifiedThread == threadID {
			continue
		}

		regs, err := p.debugapiClient.ReadRegisters(unspecifiedThread)
		if err != nil {
			return err
		}
		if err := p.SingleStep(unspecifiedThread, regs.Rip-1); err != nil {
			return err
		}
	}
	return nil
}

func (p *Process) findFieldInStruct(structAddr uint64, structType dwarf.Type, fieldName string) (dwarf.Type, []byte, error) {
	for {
		typedefType, ok := structType.(*dwarf.TypedefType)
		if !ok {
			break
		}
		structType = typedefType.Type
	}

	for _, field := range structType.(*dwarf.StructType).Field {
		if field.Name != fieldName {
			continue
		}

		buff := make([]byte, field.Type.Size())
		if err := p.debugapiClient.ReadMemory(structAddr+uint64(field.ByteOffset), buff); err != nil {
			return nil, nil, fmt.Errorf("failed to read memory: %v", err)
		}
		return field.Type, buff, nil
	}
	return nil, nil, fmt.Errorf("field %s not found", fieldName)
}

func (p *Process) findPanicHandler(gAddr, panicAddr, stackHi uint64) (*PanicHandler, error) {
	ptrToDeferType, rawVal, err := p.findFieldInStruct(gAddr, p.Binary.runtimeGType(), "_defer")
	if err != nil {
		return nil, err
	}
	deferAddr := binary.LittleEndian.Uint64(rawVal)
	deferType := ptrToDeferType.(*dwarf.PtrType).Type

	for deferAddr != 0 {
		_, rawVal, err := p.findFieldInStruct(deferAddr, deferType, "_panic")
		if err != nil {
			return nil, err
		}
		panicInDefer := binary.LittleEndian.Uint64(rawVal)
		if panicInDefer == panicAddr {
			break
		}

		_, rawVal, err = p.findFieldInStruct(deferAddr, deferType, "link")
		if err != nil {
			return nil, err
		}
		deferAddr = binary.LittleEndian.Uint64(rawVal)
	}

	if deferAddr == 0 {
		return nil, nil
	}

	_, rawVal, err = p.findFieldInStruct(deferAddr, deferType, "sp")
	if err != nil {
		return nil, err
	}
	stackAddress := binary.LittleEndian.Uint64(rawVal)
	usedStackSizeAtDefer := stackHi - stackAddress

	_, rawVal, err = p.findFieldInStruct(deferAddr, deferType, "pc")
	if err != nil {
		return nil, err
	}
	pc := binary.LittleEndian.Uint64(rawVal)

	return &PanicHandler{UsedStackSizeAtDefer: usedStackSizeAtDefer, PCAtDefer: pc}, nil
}

func (p *Process) findAncestorGoIDs(gAddr uint64) ([]int64, error) {
	if !p.GoVersion.LaterThan(GoVersion{MajorVersion: 1, MinorVersion: 11, PatchVersion: 0}) {
		// ancestors field is not supported
		return nil, nil
	}

	ptrToAncestorsType, rawVal, err := p.findFieldInStruct(gAddr, p.Binary.runtimeGType(), "ancestors")
	if err != nil || binary.LittleEndian.Uint64(rawVal) == 0 {
		return nil, err
	}

	ptrToAncestorsVal := p.valueParser.parseValue(ptrToAncestorsType, rawVal, 1)
	ancestorsVal := ptrToAncestorsVal.(ptrValue).pointedVal.(sliceValue)

	var ancestorGoIDs []int64
	for _, ancestor := range ancestorsVal.val {
		ancestorGoID := ancestor.(structValue).fields["goid"].(int64Value).val
		ancestorGoIDs = append(ancestorGoIDs, ancestorGoID)
	}
	return ancestorGoIDs, nil
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
	Name string
	Typ  dwarf.Type
	// parseValue lazily parses the value. The parsing every time is not only wasting resource, but the value may not be initialized yet.
	parseValue func(int) value
}

// ParseValue parses the arg value and returns string representation.
// The `depth` option specifies to the depth of the parsing.
func (arg Argument) ParseValue(depth int) string {
	val := arg.parseValue(depth)
	if val == nil {
		return fmt.Sprintf("%s = -", arg.Name)
	}
	return fmt.Sprintf("%s = %s", arg.Name, val)
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
