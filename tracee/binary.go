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
	// TODO: support other binary formats
	symbols []elf.Symbol
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

	symbols, err := elfFile.Symbols()
	if err != nil {
		return nil, err
	}

	return &Binary{dwarf: dwarfData, symbols: symbols}, nil
}

// Function represents a function info in the debug info section.
type Function struct {
	name       string
	parameters []Parameter
}

// Parameter represents a parameter given to or the returned from the function.
type Parameter struct {
	name     string
	typ      dwarf.Type
	location []byte
	isOutput bool
}

// FindFunction looks up the function info described in the debug info section.
func (binary *Binary) FindFunction(pc uint64) (*Function, error) {
	reader := debugInfoReader{raw: binary.dwarf.Reader(), findEntry: binary.findEntry, findType: binary.findType}
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

func (binary *Binary) findEntry(offset dwarf.Offset) *dwarf.Entry {
	reader := binary.dwarf.Reader()
	reader.Seek(offset)
	entry, err := reader.Next()
	if err != nil {
		return nil
	}
	return entry
}

func (binary *Binary) findType(offset dwarf.Offset) dwarf.Type {
	typ, err := binary.dwarf.Type(offset)
	if err != nil {
		return nil
	}
	return typ
}

type debugInfoReader struct {
	raw       *dwarf.Reader
	findEntry func(dwarf.Offset) *dwarf.Entry
	findType  func(dwarf.Offset) dwarf.Type
}

func (r debugInfoReader) seekCompileUnit(pc uint64) (*compileUnitReader, error) {
	_, err := r.raw.SeekPC(pc)
	if err != nil {
		return nil, err
	}

	// if no error, SeekPC returns the Entry for the compilation unit.
	// https://golang.org/pkg/debug/dwarf/#Reader.SeekPC
	return &compileUnitReader{raw: r.raw, findEntry: r.findEntry, findType: r.findType}, nil
}

type compileUnitReader struct {
	raw       *dwarf.Reader
	findEntry func(dwarf.Offset) *dwarf.Entry
	findType  func(dwarf.Offset) dwarf.Type
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

		if subprogram.Tag != dwarf.TagSubprogram || !r.includesPC(subprogram, pc) {
			r.raw.SkipChildren()
			continue
		}

		var name string
		err = walkUpOrigins(subprogram, r.findEntry, func(entry *dwarf.Entry) (err error) {
			name, err = stringClassAttr(entry, dwarf.AttrName)
			return err
		})
		if err != nil {
			return nil, nil, errors.New("name attr not found")
		}

		return &Function{name: name}, &subprogramReader{raw: r.raw, findEntry: r.findEntry, findType: r.findType}, nil
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
	raw       *dwarf.Reader
	findEntry func(dwarf.Offset) *dwarf.Entry
	findType  func(dwarf.Offset) dwarf.Type
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

		return r.buildParameter(param)
	}
}

func (r *subprogramReader) buildParameter(param *dwarf.Entry) (*Parameter, error) {
	var name string
	var typeOffset dwarf.Offset
	var isOutput bool
	err := walkUpOrigins(param, r.findEntry, func(entry *dwarf.Entry) (err error) {
		name, err = stringClassAttr(entry, dwarf.AttrName)
		if err != nil {
			return err
		}

		typeOffset, err = referenceClassAttr(entry, dwarf.AttrType)
		if err != nil {
			return err
		}

		isOutput, err = flagClassAttr(entry, AttrVariableParameter)
		return err
	})
	if err != nil {
		return nil, err
	}

	typ := r.findType(typeOffset)
	if typ == nil {
		return nil, fmt.Errorf("type not found: %d", typeOffset)
	}

	loc, err := locationClassAttr(param, dwarf.AttrLocation)
	if err != nil {
		return nil, errors.New("loc attr not found")
	}

	return &Parameter{name: name, typ: typ, location: loc, isOutput: isOutput}, nil
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

// walkUpOrigins follows the entry's origins until the walkFn returns *nil*.
//
// It can find the DIE of the inlined instance from the DIE of the out-of-line instance (see the DWARF spec for the terminology).
func walkUpOrigins(entry *dwarf.Entry, findEntry func(dwarf.Offset) *dwarf.Entry, walkFn func(*dwarf.Entry) error) error {
	err := walkFn(entry)
	if err == nil {
		return nil
	}

	origin := findAbstractOrigin(entry, findEntry)
	if origin == nil {
		return err
	}

	return walkUpOrigins(origin, findEntry, walkFn)
}

func findAbstractOrigin(entry *dwarf.Entry, findEntry func(dwarf.Offset) *dwarf.Entry) *dwarf.Entry {
	ref, err := referenceClassAttr(entry, dwarf.AttrAbstractOrigin)
	if err != nil {
		return nil
	}

	return findEntry(ref)
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
