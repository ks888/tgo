package tracee

import (
	"debug/dwarf"
	"testing"
)

const (
	testdataHelloworld         = "testdata/helloworld"
	testdataHelloworldMacho    = testdataHelloworld + ".macho"
	testdataHelloworldStripped = testdataHelloworld + ".stripped"
	addrMain                   = 0x482070
	addrFuncWithAbstractOrigin = 0x471340 // any function which corresponding DIE has the DW_AT_abstract_origin attribute.

	testdataParameters          = "testdata/parameters"
	addrNoParameter             = 0x482ed0
	addrOneParameter            = 0x482f40
	addrOneParameterAndVariable = 0x482fe0

	invalidAddr = 0x0
)

func TestNewBinary(t *testing.T) {
	binary, err := NewBinary(testdataHelloworld)
	if err != nil {
		t.Fatalf("failed to create new binary: %v", err)
	}

	if binary.dwarf == nil {
		t.Errorf("empty dwarf data")
	}

	if binary.functions == nil {
		t.Errorf("functions empty")
	}
}

func TestNewBinary_ProgramNotFound(t *testing.T) {
	_, err := NewBinary("./notexist")
	if err == nil {
		t.Fatal("error not returned when the path is invalid")
	}
}

func TestNewBinary_NotELFProgram(t *testing.T) {
	_, err := NewBinary(testdataHelloworldMacho)
	if err == nil {
		t.Fatal("error not returned when the binary is macho")
	}
}

func TestNewBinary_StrippedProgram(t *testing.T) {
	_, err := NewBinary(testdataHelloworldStripped)
	if err == nil {
		t.Fatal("error not returned when the binary is stripped")
	}
}

func TestFindFunction(t *testing.T) {
	binary, _ := NewBinary(testdataParameters)
	function, err := binary.FindFunction(addrOneParameter)
	if err != nil {
		t.Fatalf("failed to find function: %v", err)
	}

	if function == nil {
		t.Fatal("function is nil")
	}

	if function.parameters == nil {
		t.Fatal("parameters field is nil")
	}
}

func TestListFunctions(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	functions, err := ListFunctions(binary.dwarf)
	if err != nil {
		t.Fatalf("failed to list functions: %v", err)
	}
	if functions == nil {
		t.Fatalf("functions is nil")
	}
	hasMain := false
	for _, function := range functions {
		if function.name == "main.main" {
			hasMain = true
			break
		}
	}
	if !hasMain {
		t.Errorf("no main.main")
	}
}

func TestNext(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := subprogramReader{raw: binary.dwarf.Reader(), dwarfData: binary.dwarf}

	function, err := reader.Next(true)
	if err != nil {
		t.Fatalf("failed to get next subprogram: %v", err)
	}
	if function == nil {
		t.Fatalf("function is nil")
	}
	if function.parameters == nil {
		t.Fatalf("parameters is nil")
	}
}

func TestSeek(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := subprogramReader{raw: binary.dwarf.Reader()}

	function, err := reader.Seek(addrMain)
	if err != nil {
		t.Fatalf("failed to seek to subprogram: %v", err)
	}
	if function == nil {
		t.Fatalf("function is nil")
	}
	if function.name != "main.main" {
		t.Fatalf("invalid function name: %s", function.name)
	}
	if function.parameters != nil {
		t.Fatalf("parameters field is not nil")
	}
}

func TestSeek_InvalidPC(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := subprogramReader{raw: binary.dwarf.Reader()}

	_, err := reader.Seek(invalidAddr)
	if err == nil {
		t.Fatalf("error not returned when pc is invalid")
	}
}

func TestSeek_DIEHasAbstractOrigin(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := subprogramReader{raw: binary.dwarf.Reader(), dwarfData: binary.dwarf}

	function, _ := reader.Seek(addrFuncWithAbstractOrigin)
	if function.name != "reflect.Value.Kind" {
		t.Fatalf("invalid function name: %s", function.name)
	}
	if len(function.parameters) == 0 {
		t.Fatalf("parameter is empty")
	}
	if function.parameters[0].name != "v" {
		t.Errorf("invalid parameter name: %s", function.parameters[0].name)
	}
	if function.parameters[0].typ == nil {
		t.Errorf("empty type")
	}
	if function.parameters[0].location == nil {
		t.Errorf("empty location")
	}
	if function.parameters[0].isOutput {
		t.Errorf("wrong flag")
	}
}

func TestSeek_OneParameter(t *testing.T) {
	binary, _ := NewBinary(testdataParameters)
	reader := subprogramReader{raw: binary.dwarf.Reader(), dwarfData: binary.dwarf}

	function, err := reader.Seek(addrOneParameter)
	if err != nil {
		t.Fatalf("failed to seek to parameter: %v", err)
	}
	if function.parameters == nil {
		t.Fatalf("parameter is nil")
	}
	if len(function.parameters) != 1 {
		t.Fatalf("wrong parameters length: %d", len(function.parameters))
	}
	if function.parameters[0].name != "a" {
		t.Errorf("invalid parameter name: %s", function.parameters[0].name)
	}
	if function.parameters[0].typ == nil {
		t.Errorf("empty type")
	}
	if function.parameters[0].location == nil {
		t.Errorf("empty location")
	}
	if function.parameters[0].isOutput {
		t.Errorf("wrong flag")
	}
}

func TestSeek_HasVariableBeforeParameter(t *testing.T) {
	binary, _ := NewBinary(testdataParameters)
	reader := subprogramReader{raw: binary.dwarf.Reader(), dwarfData: binary.dwarf}

	function, err := reader.Seek(addrOneParameterAndVariable)
	if err != nil {
		t.Fatalf("failed to seek to parameter: %v", err)
	}
	if len(function.parameters) == 0 {
		t.Fatalf("parameter is nil")
	}
	if function.parameters[0].name != "i" {
		t.Errorf("invalid parameter name: %s", function.parameters[0].name)
	}
}

func TestAddressClassAttr(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := subprogramReader{raw: binary.dwarf.Reader()}
	_, _ = reader.raw.SeekPC(addrMain)
	subprogram, _ := reader.raw.Next()

	addr, err := addressClassAttr(subprogram, dwarf.AttrLowpc)
	if err != nil {
		t.Fatalf("failed to get address class: %v", err)
	}
	if addr != 0x482070 {
		t.Errorf("invalid address: %d", addr)
	}
}

func TestAddressClassAttr_InvalidAttr(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := subprogramReader{raw: binary.dwarf.Reader()}
	_, _ = reader.raw.SeekPC(addrMain)
	subprogram, _ := reader.raw.Next()

	_, err := addressClassAttr(subprogram, 0x0)
	if err == nil {
		t.Fatal("error not returned")
	}
}

func TestAddressClassAttr_InvalidClass(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := subprogramReader{raw: binary.dwarf.Reader()}
	_, _ = reader.raw.SeekPC(addrMain)
	subprogram, _ := reader.raw.Next()

	_, err := addressClassAttr(subprogram, dwarf.AttrName)
	if err == nil {
		t.Fatal("error not returned")
	}
}

func TestStringClassAttr(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := subprogramReader{raw: binary.dwarf.Reader()}
	_, _ = reader.raw.SeekPC(addrMain)
	subprogram, _ := reader.raw.Next()

	name, err := stringClassAttr(subprogram, dwarf.AttrName)
	if err != nil {
		t.Fatalf("failed to get string class: %v", err)
	}
	if name != "main.main" {
		t.Errorf("invalid name: %s", name)
	}
}

func TestReferenceClassAttr(t *testing.T) {
	binary, _ := NewBinary(testdataParameters)
	reader := subprogramReader{raw: binary.dwarf.Reader()}
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
	binary, _ := NewBinary(testdataParameters)
	reader := subprogramReader{raw: binary.dwarf.Reader()}
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
	binary, _ := NewBinary(testdataParameters)
	reader := subprogramReader{raw: binary.dwarf.Reader()}
	_, _ = reader.Next(false)
	param, _ := reader.raw.Next()

	flag, err := flagClassAttr(param, AttrVariableParameter)
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
