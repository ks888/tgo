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
	binary, err := OpenBinaryFile(testutils.ProgramHelloworld)
	if err != nil {
		t.Fatalf("failed to create new binary: %v", err)
	}

	if binary.CompiledGoVersion() == (GoVersion{}) {
		t.Errorf("empty go version")
	}
	if binary.firstModuleDataAddress() == 0 {
		t.Errorf("empty runtime.firstmoduledata type")
	}
	if binary.runtimeGType() == nil {
		t.Errorf("empty runtime.g type")
	}
}

func TestOpenBinaryFile_ProgramNotFound(t *testing.T) {
	_, err := OpenBinaryFile("./notexist")
	if err == nil {
		t.Fatal("error not returned when the path is invalid")
	}
}

func TestOpenBinaryFile_NoDwarfProgram(t *testing.T) {
	_, err := OpenBinaryFile(testutils.ProgramHelloworldNoDwarf)
	if err == nil {
		t.Fatal("error not returned when the binary has no DWARF sections")
	}
}

func TestFindFunction(t *testing.T) {
	binary, _ := OpenBinaryFile(testutils.ProgramHelloworld)
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
	binary, _ := OpenBinaryFile(testutils.ProgramHelloworld)
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
	_, dwarfData, _ := findDWARF(testutils.ProgramHelloworld)
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
	_, dwarfData, _ := findDWARF(testutils.ProgramHelloworld)
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
	_, dwarfData, _ := findDWARF(testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}

	_, err := reader.Seek(0x0)
	if err == nil {
		t.Fatalf("error not returned when pc is invalid")
	}
}

func TestSeek_DIEHasAbstractOrigin(t *testing.T) {
	_, dwarfData, _ := findDWARF(testutils.ProgramHelloworld)
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
	_, dwarfData, _ := findDWARF(testutils.ProgramHelloworld)
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
	_, dwarfData, _ := findDWARF(testutils.ProgramHelloworld)
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
	_, dwarfData, _ := findDWARF(testutils.ProgramHelloworld)
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

func TestAddressClassAttr(t *testing.T) {
	_, dwarfData, _ := findDWARF(testutils.ProgramHelloworld)
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
	_, dwarfData, _ := findDWARF(testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}
	_, _ = reader.raw.SeekPC(testutils.HelloworldAddrNoParameter)
	subprogram, _ := reader.raw.Next()

	_, err := addressClassAttr(subprogram, 0x0)
	if err == nil {
		t.Fatal("error not returned")
	}
}

func TestAddressClassAttr_InvalidClass(t *testing.T) {
	_, dwarfData, _ := findDWARF(testutils.ProgramHelloworld)
	reader := subprogramReader{raw: dwarfData.Reader(), dwarfData: dwarfData}
	_, _ = reader.raw.SeekPC(testutils.HelloworldAddrNoParameter)
	subprogram, _ := reader.raw.Next()

	_, err := addressClassAttr(subprogram, dwarf.AttrName)
	if err == nil {
		t.Fatal("error not returned")
	}
}

func TestStringClassAttr(t *testing.T) {
	_, dwarfData, _ := findDWARF(testutils.ProgramHelloworld)
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
	_, dwarfData, _ := findDWARF(testutils.ProgramHelloworld)
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
	_, dwarfData, _ := findDWARF(testutils.ProgramHelloworld)
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
	_, dwarfData, _ := findDWARF(testutils.ProgramHelloworld)
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
