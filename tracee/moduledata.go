package tracee

import (
	"debug/dwarf"
	"encoding/binary"
	"fmt"

	"github.com/ks888/tgo/log"
)

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

	if innerFtabType, ok := ftabType.(*dwarf.TypedefType); ok {
		// some go version wraps the ftab.
		ftabType = innerFtabType.Type
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
