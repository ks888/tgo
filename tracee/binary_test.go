package tracee

import (
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"fmt"
	"os"
	"os/exec"
	"reflect"
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

	if function.Parameters == nil {
		t.Fatal("parameters field is nil")
	}
}

func TestListFunctions(t *testing.T) {
	binary, _ := NewBinary(testdataParameters)
	functions, err := binary.ListFunctions()
	if err != nil {
		t.Fatalf("failed to list functions: %v", err)
	}
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
	binary, _ := NewBinary(testdataParameters)
	reader := subprogramReader{raw: binary.dwarf.Reader(), dwarfData: binary.dwarf}

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
	binary, _ := NewBinary(testdataParameters)
	reader := subprogramReader{raw: binary.dwarf.Reader()}

	function, err := reader.Seek(addrMain)
	if err != nil {
		t.Fatalf("failed to seek to subprogram: %v", err)
	}
	if function == nil {
		t.Fatalf("function is nil")
	}
	if function.Name != "main.main" {
		t.Fatalf("invalid function name: %s", function.Name)
	}
	if function.Parameters != nil {
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
	binary, _ := NewBinary(testdataParameters)
	reader := subprogramReader{raw: binary.dwarf.Reader(), dwarfData: binary.dwarf}

	function, err := reader.Seek(addrOneParameter)
	if err != nil {
		t.Fatalf("failed to seek to parameter: %v", err)
	}
	if function.Parameters == nil {
		t.Fatalf("parameter is nil")
	}
	if len(function.Parameters) != 1 {
		t.Fatalf("wrong parameters length: %d", len(function.Parameters))
	}
	if function.Parameters[0].Name != "a" {
		t.Errorf("invalid parameter name: %s", function.Parameters[0].Name)
	}
	if function.Parameters[0].Typ == nil {
		t.Errorf("empty type")
	}
	if function.Parameters[0].IsOutput {
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
	if len(function.Parameters) == 0 {
		t.Fatalf("parameter is nil")
	}
	if function.Parameters[0].Name != "i" {
		t.Errorf("invalid parameter name: %s", function.Parameters[0].Name)
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
		elfFile, err := elf.Open(testdataParameters)
		if err != nil {
			t.Fatalf("failed to open elf file: %v", err)
		}

		n, err := elfFile.Section(".debug_frame").ReadAt(actual, 0)
		if err != nil || n != len(actual) {
			t.Fatalf("failed to read CIE: %v", err)
		}
	case "darwin":
		machoFile, err := macho.Open(testdataParameters)
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
