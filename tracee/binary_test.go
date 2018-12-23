package tracee

import (
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"reflect"
	"runtime"
	"testing"

	"github.com/ks888/tgo/testutils"
)

func TestOpenBinaryFile(t *testing.T) {
	binary, err := OpenBinaryFile(testutils.ProgramHelloworld, GoVersion{})
	if err != nil {
		t.Fatalf("failed to create new binary: %v", err)
	}

	if binary.firstModuleDataAddress() == 0 {
		t.Errorf("runtime.firstmoduledata address is 0")
	}
	if binary.runtimeGType() == nil {
		t.Errorf("empty runtime.g type")
	}
}

func TestOpenBinaryFile_ProgramNotFound(t *testing.T) {
	_, err := OpenBinaryFile("./notexist", GoVersion{})
	if err == nil {
		t.Fatal("error not returned when the path is invalid")
	}
}

func TestFindFunction(t *testing.T) {
	binary, _ := OpenBinaryFile(testutils.ProgramHelloworld, GoVersion{})
	function, err := binary.FindFunction(testutils.HelloworldAddrOneParameterAndVariable)
	if err != nil {
		t.Fatalf("failed to find function: %v", err)
	}

	if function == nil {
		t.Fatal("function is nil")
	}

	if function.Parameters == nil {
		t.Fatal("parameters field is nil")
	}
}

func TestListFunctions(t *testing.T) {
	binary, _ := OpenBinaryFile(testutils.ProgramHelloworld, GoVersion{})
	functions := binary.Functions()
	if functions == nil {
		t.Fatalf("functions is nil")
	}
	hasMain := false
	for _, function := range functions {
		if function.Name == "main.main" {
			hasMain = true
			break
		}
	}
	if !hasMain {
		t.Errorf("no main.main")
	}
}

func TestIsExported(t *testing.T) {
	for i, testdata := range []struct {
		name     string
		expected bool
	}{
		{name: "fmt.Println", expected: true},
		{name: "fmt.init", expected: false},
		{name: "fmt.(*pp).Flag", expected: true},
		{name: "fmt.(*pp).fmtBool", expected: false},
		{name: "_rt0_amd64_linux", expected: false},
		{name: "type..hash.runtime.version_key", expected: false},
	} {
		function := Function{Name: testdata.name}
		actual := function.IsExported()
		if actual != testdata.expected {
			t.Errorf("[%d] wrong result: %v", i, actual)
		}
	}
}

func TestNext(t *testing.T) {
	dwarfData := findDwarfData(t, testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}

	function, err := reader.Next(true)
	if err != nil {
		t.Fatalf("failed to get next subprogram: %v", err)
	}
	if function == nil {
		t.Fatalf("function is nil")
	}
	if function.Parameters == nil {
		t.Fatalf("parameters is nil")
	}
}

func TestSeek(t *testing.T) {
	dwarfData := findDwarfData(t, testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}

	function, err := reader.Seek(testutils.HelloworldAddrOneParameterAndVariable)
	if err != nil {
		t.Fatalf("failed to seek to subprogram: %v", err)
	}
	if function == nil {
		t.Fatalf("function is nil")
	}
	if function.Name != "main.oneParameterAndOneVariable" {
		t.Errorf("invalid function name: %s", function.Name)
	}
	if function.Parameters == nil {
		t.Fatalf("parameters field is nil")
	}
	if function.Parameters[0].Name != "i" {
		t.Errorf("wrong parameter name")
	}
	if !function.Parameters[0].Exist || function.Parameters[0].Offset != 0 {
		t.Errorf("wrong parameter location: %v, %v", function.Parameters[0].Exist, function.Parameters[0].Offset)
	}
}

func TestSeek_InvalidPC(t *testing.T) {
	dwarfData := findDwarfData(t, testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}

	_, err := reader.Seek(0x0)
	if err == nil {
		t.Fatalf("error not returned when pc is invalid")
	}
}

func TestSeek_DIEHasAbstractOrigin(t *testing.T) {
	dwarfData := findDwarfData(t, testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}

	function, _ := reader.Seek(testutils.HelloworldAddrFuncWithAbstractOrigin)
	if function.Name != "reflect.Value.Kind" {
		t.Fatalf("invalid function name: %s", function.Name)
	}
	if len(function.Parameters) == 0 {
		t.Fatalf("parameter is empty")
	}
	if function.Parameters[0].Name != "v" {
		t.Errorf("invalid parameter name: %s", function.Parameters[0].Name)
	}
	if function.Parameters[0].Typ == nil {
		t.Errorf("empty type")
	}
	if function.Parameters[0].IsOutput {
		t.Errorf("wrong flag")
	}
}

func TestSeek_OneParameter(t *testing.T) {
	dwarfData := findDwarfData(t, testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}

	function, err := reader.Seek(testutils.HelloworldAddrOneParameterAndVariable)
	if err != nil {
		t.Fatalf("failed to seek to parameter: %v", err)
	}
	if function.Parameters == nil {
		t.Fatalf("parameter is nil")
	}
	if len(function.Parameters) != 1 {
		t.Fatalf("wrong parameters length: %d", len(function.Parameters))
	}
	if function.Parameters[0].Name != "i" {
		t.Errorf("invalid parameter name: %s", function.Parameters[0].Name)
	}
	if function.Parameters[0].Typ == nil {
		t.Errorf("empty type")
	}
	if function.Parameters[0].IsOutput {
		t.Errorf("wrong flag")
	}
	if !function.Parameters[0].Exist {
		t.Errorf("not exist")
	}
}

func TestSeek_HasVariableBeforeParameter(t *testing.T) {
	dwarfData := findDwarfData(t, testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}

	function, err := reader.Seek(testutils.HelloworldAddrOneParameterAndVariable)
	if err != nil {
		t.Fatalf("failed to seek to parameter: %v", err)
	}
	if len(function.Parameters) == 0 {
		t.Fatalf("parameter is nil")
	}
	if function.Parameters[0].Name != "i" {
		t.Errorf("invalid parameter name: %s", function.Parameters[0].Name)
	}
}

func TestSeek_HasTwoParameters(t *testing.T) {
	dwarfData := findDwarfData(t, testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}

	function, err := reader.Seek(testutils.HelloworldAddrTwoParameters)
	if err != nil {
		t.Fatalf("failed to seek to parameter: %v", err)
	}
	if len(function.Parameters) == 0 {
		t.Fatalf("parameter is nil")
	}
	if function.Parameters[0].Name != "j" {
		t.Errorf("invalid parameter order: %s", function.Parameters[0].Name)
	}
}

func TestModuleDataOffsets(t *testing.T) {
	binary, _ := OpenBinaryFile(testutils.ProgramHelloworld, GoVersion{})
	debuggableBinary, _ := binary.(debuggableBinaryFile)

	entry, err := debuggableBinary.findDWARFEntryByName(func(entry *dwarf.Entry) bool {
		if entry.Tag != dwarf.TagStructType {
			return false
		}
		name, err := stringClassAttr(entry, dwarf.AttrName)
		return name == "runtime.moduledata" && err == nil
	})
	if err != nil {
		t.Fatalf("no moduledata type entry: %v", err)
	}

	expectedModuleDataType, err := debuggableBinary.dwarf.Type(entry.Offset)
	if err != nil {
		t.Fatalf("no moduledata type: %v", err)
	}

	expectedFields := expectedModuleDataType.(*dwarf.StructType).Field
	for _, actualField := range moduleDataType.Field {
		for _, expectedField := range expectedFields {
			if actualField.Name == expectedField.Name {
				if actualField.ByteOffset != expectedField.ByteOffset {
					t.Errorf("wrong byte offset. expect: %d, actual: %d", expectedField.ByteOffset, actualField.ByteOffset)
				}
				if actualField.Type.Size() != expectedField.Type.Size() {
					t.Errorf("wrong size. expect: %d, actual: %d", expectedField.Type.Size(), actualField.Type.Size())
				}
				break
			}
		}
	}

	// for _, field := range expectedModuleDataType.(*dwarf.StructType).Field {
	// 	fmt.Printf("  %#v\n", field)
	// 	fmt.Printf("    %#v\n", field.Type)
	// 	if field.Name == "ftab" {
	// 		for _, innerField := range field.Type.(*dwarf.StructType).Field {
	// 			fmt.Printf("      %#v\n", innerField)
	// 			fmt.Printf("        %#v\n", innerField.Type)
	// 			if innerField.Name == "array" {
	// 				fmt.Printf("          %#v\n", innerField.Type.(*dwarf.PtrType).Type)
	// 				for _, mostInnerField := range innerField.Type.(*dwarf.PtrType).Type.(*dwarf.StructType).Field {
	// 					fmt.Printf("            %#v\n", mostInnerField)
	// 					fmt.Printf("            %#v\n", mostInnerField.Type)
	// 				}
	// 			}
	// 		}
	// 	}
	// }
}

// TODO: parse faster
// func TestParseModuleData(t *testing.T) {
// 	proc, err := LaunchProcess(testutils.ProgramTypePrint)
// 	if err != nil {
// 		t.Fatalf("failed to launch process: %v", err)
// 	}
// 	defer proc.Detach()

// 	buff := make([]byte, moduleDataType.Size())
// 	if err := proc.debugapiClient.ReadMemory(proc.Binary.firstModuleDataAddress(), buff); err != nil {
// 		t.Fatalf("failed to ReadMemory: %v", err)
// 	}

// 	val := (valueParser{reader: proc.debugapiClient}).parseValue(moduleDataType, buff, 1)
// 	for fieldName, fieldValue := range val.(structValue).fields {
// 		switch fieldName {
// 		case "pclntable", "ftab":
// 			if len(fieldValue.(sliceValue).val) == 0 {
// 				t.Errorf("empty slice: %s", fieldName)
// 			}
// 		case "findfunctab", "minpc", "types", "etypes":
// 			if fieldValue.(uint64Value).val == 0 {
// 				t.Errorf("zero value: %s", fieldName)
// 			}
// 		}
// 	}
// }

func TestAddressClassAttr(t *testing.T) {
	dwarfData := findDwarfData(t, testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}
	_, _ = reader.raw.SeekPC(testutils.HelloworldAddrNoParameter)
	subprogram, _ := reader.raw.Next()

	addr, err := addressClassAttr(subprogram, dwarf.AttrLowpc)
	if err != nil {
		t.Fatalf("failed to get address class: %v", err)
	}
	if addr != testutils.HelloworldAddrNoParameter {
		t.Errorf("invalid address: %x", addr)
	}
}

func TestAddressClassAttr_InvalidAttr(t *testing.T) {
	dwarfData := findDwarfData(t, testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}
	_, _ = reader.raw.SeekPC(testutils.HelloworldAddrNoParameter)
	subprogram, _ := reader.raw.Next()

	_, err := addressClassAttr(subprogram, 0x0)
	if err == nil {
		t.Fatal("error not returned")
	}
}

func TestAddressClassAttr_InvalidClass(t *testing.T) {
	dwarfData := findDwarfData(t, testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}
	_, _ = reader.raw.SeekPC(testutils.HelloworldAddrNoParameter)
	subprogram, _ := reader.raw.Next()

	_, err := addressClassAttr(subprogram, dwarf.AttrName)
	if err == nil {
		t.Fatal("error not returned")
	}
}

func TestStringClassAttr(t *testing.T) {
	dwarfData := findDwarfData(t, testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}
	_, _ = reader.raw.SeekPC(testutils.HelloworldAddrNoParameter)
	subprogram, _ := reader.raw.Next()

	name, err := stringClassAttr(subprogram, dwarf.AttrName)
	if err != nil {
		t.Fatalf("failed to get string class: %v", err)
	}
	if name != "main.noParameter" {
		t.Errorf("invalid name: %s", name)
	}
}

func TestReferenceClassAttr(t *testing.T) {
	dwarfData := findDwarfData(t, testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}
	_, _ = reader.Next(false)
	param, _ := reader.raw.Next()

	ref, err := referenceClassAttr(param, dwarf.AttrType)
	if err != nil {
		t.Fatalf("failed to get reference class: %v", err)
	}
	if ref == 0 {
		t.Errorf("invalid reference")
	}
}

func TestLocClassAttr(t *testing.T) {
	dwarfData := findDwarfData(t, testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}
	_, _ = reader.Next(false)
	param, _ := reader.raw.Next()

	loc, err := locationClassAttr(param, dwarf.AttrLocation)
	if err != nil {
		t.Fatalf("failed to get location class: %v", err)
	}
	if loc == nil {
		t.Errorf("invalid loc")
	}
}

func TestFlagClassAttr(t *testing.T) {
	dwarfData := findDwarfData(t, testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}
	_, _ = reader.Next(false)
	param, _ := reader.raw.Next()

	flag, err := flagClassAttr(param, attrVariableParameter)
	if err != nil {
		t.Fatalf("failed to get location class: %v", err)
	}
	if flag {
		t.Errorf("invalid flag")
	}
}

func TestDecodeSignedLEB128(t *testing.T) {
	for _, data := range []struct {
		input    []byte
		expected int
	}{
		{input: []byte{0x02}, expected: 2},
		{input: []byte{0x7e}, expected: -2},
		{input: []byte{0xff, 0x00}, expected: 127},
		{input: []byte{0x81, 0x7f}, expected: -127},
		{input: []byte{0x80, 0x01}, expected: 128},
		{input: []byte{0x80, 0x7f}, expected: -128},
	} {
		actual := decodeSignedLEB128(data.input)
		if data.expected != actual {
			t.Errorf("actual: %d expected: %d", actual, data.expected)
		}
	}
}

// This test checks if the binary has the dwarf_frame section and its Common Information Entry is not changed.
// AFAIK, the entry is rarely changed and so the check is skipped at runtime.
func TestDebugFrameSection(t *testing.T) {
	/*
		00000000 0000000000000010 ffffffff CIE
		  Version:               3
		  Augmentation:          ""
		  Code alignment factor: 1
		  Data alignment factor: -4
		  Return address column: 16

		  DW_CFA_def_cfa: r7 (rsp) ofs 8
		  DW_CFA_offset_extended: r16 (rip) at cfa-8
		  DW_CFA_nop
	*/
	expectedCIE := []byte{0x10, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff, 0x03, 0x00, 0x01, 0x7c, 0x10, 0x0c, 0x07, 0x08, 0x05, 0x10, 0x02, 0x00}
	actual := make([]byte, len(expectedCIE))

	switch runtime.GOOS {
	case "linux":
		elfFile, err := elf.Open(testutils.ProgramHelloworld)
		if err != nil {
			t.Fatalf("failed to open elf file: %v", err)
		}

		n, err := elfFile.Section(".debug_frame").ReadAt(actual, 0)
		if err != nil || n != len(actual) {
			t.Fatalf("failed to read CIE: %v", err)
		}
	case "darwin":
		machoFile, err := macho.Open(testutils.ProgramHelloworld)
		if err != nil {
			t.Fatalf("failed to open macho file: %v", err)
		}

		n, err := machoFile.Section("__debug_frame").ReadAt(actual, 0)
		if err != nil || n != len(actual) {
			t.Fatalf("failed to read CIE: %v", err)
		}
	default:
		t.Fatalf("unsupported os: %s", runtime.GOOS)
	}

	if !reflect.DeepEqual(expectedCIE, actual) {
		t.Errorf("CIE changed: %v", actual)
	}
}

func TestOpenNonDwarfBinaryFile(t *testing.T) {
	binary, err := OpenBinaryFile(testutils.ProgramHelloworldNoDwarf, GoVersion{})
	if err != nil {
		t.Fatalf("failed to create new binary: %v", err)
	}
	if _, err := binary.FindFunction(0); err == nil {
		t.Errorf("FindFunction doesn't return error")
	}
	if funcs := binary.Functions(); len(funcs) == 0 {
		t.Errorf("Functions return empty list")
	}
	if binary.firstModuleDataAddress() == 0 {
		t.Errorf("runtime.firstmoduledata address is 0")
	}
	// binary.findDwarfTypeByAddr()
	// if binary.runtimeGType() == nil {
	// 	t.Errorf("empty runtime.g type")
	// }
}

func findDwarfData(t *testing.T, pathToProgram string) dwarfData {
	binaryFile, err := openBinaryFile(pathToProgram, GoVersion{})
	if err != nil {
		t.Fatalf("failed to open: %v", err)
	}

	if debuggableBinary, ok := binaryFile.(debuggableBinaryFile); ok {
		return debuggableBinary.dwarf
	}
	return dwarfData{}
}
