package tracee

import (
	"debug/dwarf"
	"debug/elf"
	"errors"
	"fmt"
)

// AttrVariableParameter is the extended DWARF attribute. If true, the parameter is output. Else, it's input.
const AttrVariableParameter = 0x4b

// Binary represents the program the tracee process executes
type Binary struct {
	dwarf *dwarf.Data
}

// NewBinary returns the new binary object associated to the program.
func NewBinary(pathToProgram string) (*Binary, error) {
	elfFile, err := elf.Open(pathToProgram)
	if err != nil {
		return nil, err
	}

	dwarfData, err := elfFile.DWARF()
	if err != nil {
		return nil, err
	}

	// TODO: symbol table

	return &Binary{dwarf: dwarfData}, nil
}

// Function represents a function info in the debug info section.
type Function struct {
	name       string
	parameters []Parameter
}

// Parameter represents a parameter given to or the returned from the function.
type Parameter struct {
	name       string
	typeOffset dwarf.Offset
	location   []byte
	isOutput   bool
}

// FindFunction looks up the function info described in the debug info section.
func (binary *Binary) FindFunction(pc uint64) (*Function, error) {
	reader := debugInfoReader{binary.dwarf.Reader()}
	unitReader, err := reader.seekCompileUnit(pc)
	if err != nil {
		return nil, err
	}

	function, subprogReader, err := unitReader.seekSubprogram(pc)
	if err != nil {
		return nil, err
	}

	params, err := subprogReader.seekParameters()
	if err != nil {
		return nil, err
	}
	function.parameters = params

	return function, nil
}

type debugInfoReader struct {
	raw *dwarf.Reader
}

func (r debugInfoReader) seekCompileUnit(pc uint64) (*compileUnitReader, error) {
	_, err := r.raw.SeekPC(pc)
	if err != nil {
		return nil, err
	}

	// if no error, SeekPC returns the Entry for the compilation unit.
	// https://golang.org/pkg/debug/dwarf/#Reader.SeekPC
	return &compileUnitReader{raw: r.raw}, nil
}

type compileUnitReader struct {
	raw *dwarf.Reader
}

func (r *compileUnitReader) seekSubprogram(pc uint64) (*Function, *subprogramReader, error) {
	for {
		subprogram, err := r.raw.Next()
		if err != nil {
			return nil, nil, err
		}
		if subprogram == nil {
			return nil, nil, errors.New("subprogram not found")
		}

		// TODO: support inlined case

		if subprogram.Tag != dwarf.TagSubprogram || !r.includesPC(subprogram, pc) {
			r.raw.SkipChildren()
			continue
		}

		name, err := stringClassAttr(subprogram, dwarf.AttrName)
		if err != nil {
			return nil, nil, errors.New("name attr not found")
		}

		return &Function{name: name}, &subprogramReader{raw: r.raw}, nil
	}
}

func (r *compileUnitReader) includesPC(subprogram *dwarf.Entry, pc uint64) bool {
	lowPC, err := addressClassAttr(subprogram, dwarf.AttrLowpc)
	if err != nil {
		return false
	}

	highPC, err := addressClassAttr(subprogram, dwarf.AttrHighpc)
	if err != nil {
		return false
	}

	if pc < lowPC || highPC <= pc {
		return false
	}
	return true
}

type subprogramReader struct {
	raw *dwarf.Reader
}

func (r *subprogramReader) seekParameters() ([]Parameter, error) {
	var params []Parameter
	for {
		param, err := r.seekParameter()
		if err != nil || param == nil {
			return params, err
		}

		params = append(params, *param)
		r.raw.SkipChildren()
	}
}

func (r *subprogramReader) seekParameter() (*Parameter, error) {
	for {
		param, err := r.raw.Next()
		if err != nil || param.Tag == 0 {
			return nil, err
		}

		if param.Tag != dwarf.TagFormalParameter {
			r.raw.SkipChildren()
			continue
		}

		name, err := stringClassAttr(param, dwarf.AttrName)
		if err != nil {
			return nil, errors.New("name attr not found")
		}

		typeOffset, err := referenceClassAttr(param, dwarf.AttrType)
		if err != nil {
			return nil, errors.New("type attr not found")
		}

		loc, err := locationClassAttr(param, dwarf.AttrLocation)
		if err != nil {
			return nil, errors.New("loc attr not found")
		}

		isOutput, err := flagClassAttr(param, AttrVariableParameter)
		if err != nil {
			return nil, errors.New("variable parameter attr not found")
		}

		return &Parameter{name: name, typeOffset: typeOffset, location: loc, isOutput: isOutput}, nil
	}
}

func addressClassAttr(entry *dwarf.Entry, attrName dwarf.Attr) (uint64, error) {
	field := entry.AttrField(attrName)
	if field == nil {
		return 0, errors.New("attr not found")
	}

	if field.Class != dwarf.ClassAddress {
		return 0, fmt.Errorf("invalid class: %v", field.Class)
	}

	// https://golang.org/pkg/debug/dwarf/#Field
	val := field.Val.(uint64)
	return val, nil
}

func stringClassAttr(entry *dwarf.Entry, attrName dwarf.Attr) (string, error) {
	field := entry.AttrField(attrName)
	if field == nil {
		return "", errors.New("attr not found")
	}

	if field.Class != dwarf.ClassString {
		return "", fmt.Errorf("invalid class: %v", field.Class)
	}

	// https://golang.org/pkg/debug/dwarf/#Field
	val := field.Val.(string)
	return val, nil
}

func referenceClassAttr(entry *dwarf.Entry, attrName dwarf.Attr) (dwarf.Offset, error) {
	field := entry.AttrField(attrName)
	if field == nil {
		return 0, errors.New("attr not found")
	}

	if field.Class != dwarf.ClassReference {
		return 0, fmt.Errorf("invalid class: %v", field.Class)
	}

	// https://golang.org/pkg/debug/dwarf/#Field
	val := field.Val.(dwarf.Offset)
	return val, nil
}

func locationClassAttr(entry *dwarf.Entry, attrName dwarf.Attr) ([]byte, error) {
	field := entry.AttrField(attrName)
	if field == nil {
		return nil, errors.New("attr not found")
	}

	if field.Class != dwarf.ClassExprLoc {
		return nil, fmt.Errorf("invalid class: %v", field.Class)
	}

	// https://golang.org/pkg/debug/dwarf/#Field
	val := field.Val.([]byte)
	return val, nil
}

func flagClassAttr(entry *dwarf.Entry, attrName dwarf.Attr) (bool, error) {
	field := entry.AttrField(attrName)
	if field == nil {
		return false, errors.New("attr not found")
	}

	if field.Class != dwarf.ClassFlag {
		return false, fmt.Errorf("invalid class: %v", field.Class)
	}

	// https://golang.org/pkg/debug/dwarf/#Field
	val := field.Val.(bool)
	return val, nil
}

func decodeSignedLEB128(input []byte) (val int) {
	var i int
	for {
		val |= int(input[i]) & 0x7F << (7 * uint(i))

		if input[i]>>7&0x1 == 0x0 {
			break
		}
		i++
	}

	if input[i]>>6&0x1 == 0x1 {
		// negative value
		return (^0)<<((uint(i)+1)*7) + val
	}
	return val
}
