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
		t.Errorf("empty data: %v", binary)
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

func TestSeekCompileUnit(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := debugInfoReader{binary.dwarf.Reader()}

	compileUnitReader, err := reader.seekCompileUnit(addrMain)
	if err != nil {
		t.Fatalf("failed to seek to compile unit: %v", err)
	}
	if compileUnitReader == nil || compileUnitReader.raw == nil {
		t.Fatal("compile unit reader is nil")
	}
}

func TestSeekCompileUnit_InvalidPC(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := debugInfoReader{binary.dwarf.Reader()}

	_, err := reader.seekCompileUnit(invalidAddr)
	if err == nil {
		t.Fatalf("error not returned when pc is invalid")
	}
}

func TestSeekSubprogram(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := debugInfoReader{binary.dwarf.Reader()}

	compileUnitReader, _ := reader.seekCompileUnit(addrMain)
	function, subprogramReader, err := compileUnitReader.seekSubprogram(addrMain)
	if err != nil {
		t.Fatalf("failed to seek to subprogram: %v", err)
	}
	if function == nil {
		t.Fatalf("function is nil")
	}
	if function.name != "main.main" {
		t.Fatalf("invalid function name: %s", function.name)
	}
	if subprogramReader == nil {
		t.Fatalf("subprogramReader is nil")
	}
}

func TestSeekSubprogram_NoMatchedSubprogram(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := debugInfoReader{binary.dwarf.Reader()}

	compileUnitReader, _ := reader.seekCompileUnit(addrMain)
	_, _, err := compileUnitReader.seekSubprogram(invalidAddr)
	if err == nil {
		t.Fatalf("error not returned when no matched subprogram")
	}
}

func TestSeekParameters(t *testing.T) {
	binary, _ := NewBinary(testdataParameters)
	reader := debugInfoReader{binary.dwarf.Reader()}

	compileUnitReader, _ := reader.seekCompileUnit(addrOneParameter)
	_, subprogramReader, _ := compileUnitReader.seekSubprogram(addrOneParameter)
	params, err := subprogramReader.seekParameters()
	if err != nil {
		t.Fatalf("failed to seek to parameter: %v", err)
	}
	if params == nil {
		t.Fatalf("parameter is nil")
	}
	if len(params) != 1 {
		t.Fatalf("wrong parameters length: %d", len(params))
	}
}

func TestSeekParameter(t *testing.T) {
	binary, _ := NewBinary(testdataParameters)
	reader := debugInfoReader{binary.dwarf.Reader()}

	compileUnitReader, _ := reader.seekCompileUnit(addrOneParameter)
	_, subprogramReader, _ := compileUnitReader.seekSubprogram(addrOneParameter)
	param, err := subprogramReader.seekParameter()
	if err != nil {
		t.Fatalf("failed to seek to parameter: %v", err)
	}
	if param == nil {
		t.Fatalf("parameter is nil")
	}
	if param.name != "a" {
		t.Errorf("invalid parameter name: %s", param.name)
	}
	if param.typeOffset == 0 {
		t.Errorf("empty type offset")
	}
	if param.location == nil {
		t.Errorf("empty location")
	}
	if param.isOutput {
		t.Errorf("wrong flag")
	}
}

func TestSeekParameter_HasVariableBeforeParameter(t *testing.T) {
	binary, _ := NewBinary(testdataParameters)
	reader := debugInfoReader{binary.dwarf.Reader()}

	compileUnitReader, _ := reader.seekCompileUnit(addrOneParameterAndVariable)
	_, subprogramReader, _ := compileUnitReader.seekSubprogram(addrOneParameterAndVariable)
	param, err := subprogramReader.seekParameter()
	if err != nil {
		t.Fatalf("failed to seek to parameter: %v", err)
	}
	if param == nil {
		t.Fatalf("parameter is nil")
	}
	if param.name != "i" {
		t.Errorf("invalid parameter name: %s", param.name)
	}
}

func TestSeekParameter_NoParameter(t *testing.T) {
	binary, _ := NewBinary(testdataParameters)
	reader := debugInfoReader{binary.dwarf.Reader()}

	compileUnitReader, _ := reader.seekCompileUnit(addrNoParameter)
	_, subprogramReader, _ := compileUnitReader.seekSubprogram(addrNoParameter)
	param, err := subprogramReader.seekParameter()
	if err != nil {
		t.Fatalf("failed to seek to parameter: %v", err)
	}
	if param != nil {
		t.Fatalf("parameter is not nil when no parameter")
	}
}

func TestAddressClassAttr(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := debugInfoReader{binary.dwarf.Reader()}
	compileUnitReader, _ := reader.seekCompileUnit(addrMain)
	subprogram, _ := compileUnitReader.raw.Next()

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
	reader := debugInfoReader{binary.dwarf.Reader()}
	compileUnitReader, _ := reader.seekCompileUnit(addrMain)
	subprogram, _ := compileUnitReader.raw.Next()

	_, err := addressClassAttr(subprogram, 0x0)
	if err == nil {
		t.Fatal("error not returned")
	}
}

func TestAddressClassAttr_InvalidClass(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := debugInfoReader{binary.dwarf.Reader()}
	compileUnitReader, _ := reader.seekCompileUnit(addrMain)
	subprogram, _ := compileUnitReader.raw.Next()

	_, err := addressClassAttr(subprogram, dwarf.AttrName)
	if err == nil {
		t.Fatal("error not returned")
	}
}

func TestStringClassAttr(t *testing.T) {
	binary, _ := NewBinary(testdataHelloworld)
	reader := debugInfoReader{binary.dwarf.Reader()}
	compileUnitReader, _ := reader.seekCompileUnit(addrMain)
	subprogram, _ := compileUnitReader.raw.Next()

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
	reader := debugInfoReader{binary.dwarf.Reader()}

	compileUnitReader, _ := reader.seekCompileUnit(addrOneParameter)
	_, subprogramReader, _ := compileUnitReader.seekSubprogram(addrOneParameter)
	param, _ := subprogramReader.raw.Next()

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
	reader := debugInfoReader{binary.dwarf.Reader()}

	compileUnitReader, _ := reader.seekCompileUnit(addrOneParameter)
	_, subprogramReader, _ := compileUnitReader.seekSubprogram(addrOneParameter)
	param, _ := subprogramReader.raw.Next()

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
	reader := debugInfoReader{binary.dwarf.Reader()}

	compileUnitReader, _ := reader.seekCompileUnit(addrOneParameter)
	_, subprogramReader, _ := compileUnitReader.seekSubprogram(addrOneParameter)
	param, _ := subprogramReader.raw.Next()

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
