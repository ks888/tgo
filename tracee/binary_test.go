package tracee

import (
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"testing"
)

var (
	testdataParameters                 = "testdata/parameters"
	testdataParametersGo               = testdataParameters + ".go"
	testdataParametersNoDwarf          = testdataParameters + ".nodwarf"
	addrMain                    uint64 = 0x0
	addrNoParameter             uint64 = 0x0
	addrOneParameter            uint64 = 0x0
	addrOneParameterAndVariable uint64 = 0x0
	addrFuncWithAbstractOrigin  uint64 = 0x0 // any function which corresponding DIE has the DW_AT_abstract_origin attribute.
)

func TestMain(m *testing.M) {
	if err := buildTestProgram(); err != nil {
		fmt.Printf("ERROR: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func buildTestProgram() error {
	if out, err := exec.Command("go", "build", "-o", testdataParameters, testdataParametersGo).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build %s: %v\n%v", testdataParametersGo, err, string(out))
	}

	if out, err := exec.Command("go", "build", "-ldflags", "-w", "-o", testdataParametersNoDwarf, testdataParametersGo).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build %s: %v\n%v", testdataParametersGo, err, string(out))
	}

	switch runtime.GOOS {
	case "darwin":
		machoFile, err := macho.Open(testdataParameters)
		if err != nil {
			return fmt.Errorf("failed to open binary: %v", err)
		}
		for _, sym := range machoFile.Symtab.Syms {
			updateAddressIfMatched(sym.Name, sym.Value)
		}

	case "linux":
		elfFile, err := elf.Open(testdataParameters)
		if err != nil {
			return fmt.Errorf("failed to open binary: %v", err)
		}
		syms, err := elfFile.Symbols()
		if err != nil {
			return fmt.Errorf("failed to find symbols: %v", err)
		}
		for _, sym := range syms {
			updateAddressIfMatched(sym.Name, sym.Value)
		}
	default:
		return fmt.Errorf("unsupported os: %s", runtime.GOOS)
	}

	return nil
}

// not used yet for simplicity.
func needBuild() bool {
	fiBinary, err := os.Stat(testdataParameters)
	if os.IsNotExist(err) {
		return true
	}

	switch runtime.GOOS {
	case "darwin":
		if _, err := macho.Open(testdataParameters); err != nil {
			return true
		}
	case "linux":
		if _, err := elf.Open(testdataParameters); err != nil {
			return true
		}
	default:
		return true
	}

	fiSrc, _ := os.Stat(testdataParametersGo)
	return fiSrc.ModTime().After(fiBinary.ModTime())
}

func updateAddressIfMatched(name string, value uint64) {
	switch name {
	case "main.main":
		addrMain = value
	case "main.oneParameter":
		addrOneParameter = value
	case "main.oneParameterAndOneVariable":
		addrOneParameterAndVariable = value
	case "main.noParameter":
		addrNoParameter = value
	case "reflect.Value.Kind":
		addrFuncWithAbstractOrigin = value
	}
}

func TestNewBinary(t *testing.T) {
	binary, err := NewBinary(testdataParameters)
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

func TestNewBinary_NoDwarfProgram(t *testing.T) {
	_, err := NewBinary(testdataParametersNoDwarf)
	if err == nil {
		t.Fatal("error not returned when the binary has no DWARF sections")
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
	binary, _ := NewBinary(testdataParameters)
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
	binary, _ := NewBinary(testdataParameters)
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
	binary, _ := NewBinary(testdataParameters)
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
	binary, _ := NewBinary(testdataParameters)
	reader := subprogramReader{raw: binary.dwarf.Reader()}

	_, err := reader.Seek(0x0)
	if err == nil {
		t.Fatalf("error not returned when pc is invalid")
	}
}

func TestSeek_DIEHasAbstractOrigin(t *testing.T) {
	binary, _ := NewBinary(testdataParameters)
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
	binary, _ := NewBinary(testdataParameters)
	reader := subprogramReader{raw: binary.dwarf.Reader()}
	_, _ = reader.raw.SeekPC(addrNoParameter)
	subprogram, _ := reader.raw.Next()

	addr, err := addressClassAttr(subprogram, dwarf.AttrLowpc)
	if err != nil {
		t.Fatalf("failed to get address class: %v", err)
	}
	if addr != addrNoParameter {
		t.Errorf("invalid address: %x", addr)
	}
}

func TestAddressClassAttr_InvalidAttr(t *testing.T) {
	binary, _ := NewBinary(testdataParameters)
	reader := subprogramReader{raw: binary.dwarf.Reader()}
	_, _ = reader.raw.SeekPC(addrNoParameter)
	subprogram, _ := reader.raw.Next()

	_, err := addressClassAttr(subprogram, 0x0)
	if err == nil {
		t.Fatal("error not returned")
	}
}

func TestAddressClassAttr_InvalidClass(t *testing.T) {
	binary, _ := NewBinary(testdataParameters)
	reader := subprogramReader{raw: binary.dwarf.Reader()}
	_, _ = reader.raw.SeekPC(addrNoParameter)
	subprogram, _ := reader.raw.Next()

	_, err := addressClassAttr(subprogram, dwarf.AttrName)
	if err == nil {
		t.Fatal("error not returned")
	}
}

func TestStringClassAttr(t *testing.T) {
	binary, _ := NewBinary(testdataParameters)
	reader := subprogramReader{raw: binary.dwarf.Reader()}
	_, _ = reader.raw.SeekPC(addrNoParameter)
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
