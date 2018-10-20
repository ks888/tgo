package tracee

import (
	"debug/dwarf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"unicode"
)

const (
	// AttrVariableParameter is the extended DWARF attribute. If true, the parameter is output. Else, it's input.
	attrVariableParameter = 0x4b
	attrGoRuntimeType     = 0x2904 // DW_AT_go_runtime_type
	dwarfOpCallFrameCFA   = 0x9c   // DW_OP_call_frame_cfa
	dwarfOpFbreg          = 0x91   // DW_OP_fbreg
)

// Binary represents the program the tracee process executes
type Binary struct {
	dwarf     *dwarf.Data
	closer    io.Closer
	goVersion GoVersion
	types     map[uint64]dwarf.Offset
}

// Function represents a function info in the debug info section.
type Function struct {
	Name       string
	Value      uint64
	Parameters []Parameter
}

// Parameter represents a parameter given to or the returned from the function.
type Parameter struct {
	Name string
	Typ  dwarf.Type
	// Offset is the memory offset of the parameter.
	Offset int
	// Exist is false when the parameter is removed due to the optimization.
	Exist    bool
	IsOutput bool
}

// NewBinary returns the new binary object associated to the program.
func NewBinary(pathToProgram string) (Binary, error) {
	closer, dwarfData, err := findDWARF(pathToProgram)
	if err != nil {
		return Binary{}, err
	}
	binary := Binary{dwarf: dwarfData, closer: closer}

	binary.types, err = binary.buildTypes()
	if err != nil {
		return Binary{}, err
	}
	binary.goVersion = binary.findGoVersion()
	return binary, nil
}

func (b Binary) buildTypes() (map[uint64]dwarf.Offset, error) {
	types := make(map[uint64]dwarf.Offset)
	reader := b.dwarf.Reader()
	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			return types, err
		}

		switch entry.Tag {
		case dwarf.TagArrayType, dwarf.TagPointerType, dwarf.TagStructType, dwarf.TagSubroutineType, dwarf.TagBaseType, dwarf.TagTypedef:
			// based on the 'abbrevs' variable in src/cmd/internal/dwarf/dwarf.go. It indicates which tag types may have the DW_AT_go_runtime_type attribute.
			val, err := addressClassAttr(entry, attrGoRuntimeType)
			if err != nil || val == 0 {
				break
			}
			types[val] = entry.Offset
		}
	}
}

func (b Binary) findGoVersion() GoVersion {
	const producerPrefix = "Go cmd/compile "

	reader := b.dwarf.Reader()
	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			return GoVersion{}
		}

		if entry.Tag != dwarf.TagCompileUnit {
			reader.SkipChildren()
			continue
		}

		producer, err := stringClassAttr(entry, dwarf.AttrProducer)
		if !strings.HasPrefix(producer, producerPrefix) {
			reader.SkipChildren()
			continue
		}

		return ParseGoVersion(strings.TrimPrefix(producer, producerPrefix))
	}
}

const firstModuleDataName = "runtime.firstmoduledata"

func (b Binary) findFirstModuleDataAddress() (uint64, error) {
	reader := b.dwarf.Reader()
	for {
		entry, err := reader.Next()
		if err != nil {
			return 0, err
		} else if entry == nil {
			return 0, errors.New("failed to find firstmoduledata")
		}

		name, err := stringClassAttr(entry, dwarf.AttrName)
		if err != nil || name != firstModuleDataName {
			continue
		}

		loc, err := locationClassAttr(entry, dwarf.AttrLocation)
		if err != nil {
			return 0, err
		}
		if len(loc) == 0 || loc[0] != 0x3 {
			return 0, fmt.Errorf("unexpected location format: %v", loc)
		}
		return binary.LittleEndian.Uint64(loc[1:len(loc)]), nil
	}
}

// Close releases the resources associated with the binary.
func (b Binary) Close() error {
	return b.closer.Close()
}

// FindFunction looks up the function info described in the debug info section.
func (b Binary) FindFunction(pc uint64) (*Function, error) {
	reader := subprogramReader{raw: b.dwarf.Reader(), dwarfData: b.dwarf}
	return reader.Seek(pc)
}

// ListFunctions lists the subprograms in the debug info section. They don't include parameters info.
func (b Binary) ListFunctions() ([]*Function, error) {
	reader := subprogramReader{raw: b.dwarf.Reader(), dwarfData: b.dwarf}

	var funcs []*Function
	for {
		function, err := reader.Next(false)
		if err != nil {
			return nil, err
		}
		if function == nil {
			return funcs, nil
		}
		funcs = append(funcs, function)
	}
}

// IsExported returns true if the function is exported.
// See https://golang.org/ref/spec#Exported_identifiers for the spec.
func (f Function) IsExported() bool {
	elems := strings.Split(f.Name, ".")
	for _, ch := range elems[len(elems)-1] {
		return unicode.IsUpper(ch)
	}
	return false
}

type subprogramReader struct {
	raw       *dwarf.Reader
	dwarfData *dwarf.Data
}

func (r subprogramReader) Next(setParameters bool) (*Function, error) {
	for {
		entry, err := r.raw.Next()
		if err != nil || entry == nil {
			return nil, err
		}

		if entry.Tag != dwarf.TagSubprogram || r.isInline(entry) {
			continue
		}

		function, err := r.buildFunction(entry)
		if err != nil {
			return nil, err
		}

		if setParameters {
			function.Parameters, err = r.parameters()
		}
		return function, err

	}
}

func (r subprogramReader) Seek(pc uint64) (*Function, error) {
	_, err := r.raw.SeekPC(pc)
	if err != nil {
		return nil, err
	}

	for {
		subprogram, err := r.raw.Next()
		if err != nil {
			return nil, err
		}
		if subprogram == nil {
			return nil, errors.New("subprogram not found")
		}

		if subprogram.Tag != dwarf.TagSubprogram || !r.includesPC(subprogram, pc) {
			r.raw.SkipChildren()
			continue
		}

		function, err := r.buildFunction(subprogram)
		if err != nil {
			return nil, err
		}

		function.Parameters, err = r.parameters()
		// Without this, the parameters are sorted by the name.
		// TODO: maybe sorted by offset in go 1.11
		sort.Slice(function.Parameters, func(i, j int) bool { return function.Parameters[i].Offset < function.Parameters[j].Offset })
		return function, err
	}
}

func (r subprogramReader) includesPC(subprogram *dwarf.Entry, pc uint64) bool {
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

func (r subprogramReader) isInline(subprogram *dwarf.Entry) bool {
	return subprogram.AttrField(dwarf.AttrInline) != nil
}

func (r subprogramReader) buildFunction(subprogram *dwarf.Entry) (*Function, error) {
	var name string
	err := walkUpOrigins(subprogram, r.dwarfData, func(entry *dwarf.Entry) bool {
		var err error
		name, err = stringClassAttr(entry, dwarf.AttrName)
		return err == nil
	})
	if err != nil {
		return nil, errors.New("name attr not found")
	}

	lowPC, err := addressClassAttr(subprogram, dwarf.AttrLowpc)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	}

	_, err = addressClassAttr(subprogram, dwarf.AttrHighpc)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	}

	frameBase, err := locationClassAttr(subprogram, dwarf.AttrFrameBase)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	} else if len(frameBase) != 1 || frameBase[0] != dwarfOpCallFrameCFA {
		log.Printf("The frame base attribute of %s has the unexpected value. The parameter values may be wrong.", name)
	}

	return &Function{Name: name, Value: lowPC}, nil
}

func (r subprogramReader) parameters() ([]Parameter, error) {
	var params []Parameter
	for {
		param, err := r.nextParameter()
		if err != nil || param == nil {
			return params, err
		}

		params = append(params, *param)
		r.raw.SkipChildren()
	}
}

func (r subprogramReader) nextParameter() (*Parameter, error) {
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

func (r subprogramReader) buildParameter(param *dwarf.Entry) (*Parameter, error) {
	var name string
	var typeOffset dwarf.Offset
	var isOutput bool
	err := walkUpOrigins(param, r.dwarfData, func(entry *dwarf.Entry) bool {
		var err error
		name, err = stringClassAttr(entry, dwarf.AttrName)
		if err != nil {
			return false
		}

		typeOffset, err = referenceClassAttr(entry, dwarf.AttrType)
		if err != nil {
			return false
		}

		isOutput, err = flagClassAttr(entry, attrVariableParameter)
		return err == nil
	})
	if err != nil {
		return nil, err
	}

	typ, err := r.dwarfData.Type(typeOffset)
	if err != nil {
		return nil, err
	}

	offset, exist, err := offsetWithLocationDesc(param)
	if err != nil {
		if _, _, err := offsetWithLocationList(param); err != nil {
			return nil, err
		}
	}
	return &Parameter{Name: name, Typ: typ, Offset: offset, IsOutput: isOutput, Exist: exist}, nil
}

func offsetWithLocationDesc(param *dwarf.Entry) (offset int, exist bool, err error) {
	loc, err := locationClassAttr(param, dwarf.AttrLocation)
	if err != nil {
		return 0, false, fmt.Errorf("loc attr not found: %v", err)
	}

	if len(loc) > 0 {
		offset, err := parameterOffset(loc)
		return offset, true, err
	}
	// the location description may be empty due to the optimization (see the DWARF spec 2.6.1.1.4)
	return offset, false, nil
}

// parameterOffset returns the memory offset of the parameter value.
// It's not the canonical way to interpret the location attribute, because we need to parse
// the CIE and FDE of the debug_frame section to find the current CFA.
// Here, we simply assume the CFA is the rsp+8 because we will not get the parameter value
// in the middle of the funciton.
func parameterOffset(loc []byte) (int, error) {
	// TODO: support location list case
	switch loc[0] {
	case dwarfOpCallFrameCFA:
		return 0, nil
	case dwarfOpFbreg:
		return decodeSignedLEB128(loc[1:]), nil
	default:
		return 0, fmt.Errorf("unknown operation: %v", loc[0])
	}
}

func offsetWithLocationList(param *dwarf.Entry) (int, bool, error) {
	_, err := locationListClassAttr(param, dwarf.AttrLocation)
	if err != nil {
		return 0, false, fmt.Errorf("loc list attr not found: %v", err)
	}

	// TODO: support location list
	return 0, true, nil
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

func locationListClassAttr(entry *dwarf.Entry, attrName dwarf.Attr) (int64, error) {
	field := entry.AttrField(attrName)
	if field == nil {
		return 0, errors.New("attr not found")
	}

	if field.Class != dwarf.ClassLocListPtr {
		return 0, fmt.Errorf("invalid class: %v", field.Class)
	}

	// https://golang.org/pkg/debug/dwarf/#Field
	val := field.Val.(int64)
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

// walkUpOrigins follows the entry's origins until the walkFn returns true.
//
// It can find the DIE of the inlined instance from the DIE of the out-of-line instance (see the DWARF spec for the terminology).
func walkUpOrigins(entry *dwarf.Entry, dwarfData *dwarf.Data, walkFn func(*dwarf.Entry) bool) error {
	if ok := walkFn(entry); ok {
		return nil
	}

	origin := findAbstractOrigin(entry, dwarfData)
	if origin == nil {
		return errors.New("failed to find abstract origin")
	}

	return walkUpOrigins(origin, dwarfData, walkFn)
}

func findAbstractOrigin(entry *dwarf.Entry, dwarfData *dwarf.Data) *dwarf.Entry {
	ref, err := referenceClassAttr(entry, dwarf.AttrAbstractOrigin)
	if err != nil {
		return nil
	}

	reader := dwarfData.Reader()
	reader.Seek(ref)
	originEntry, err := reader.Next()
	if err != nil {
		return nil
	}
	return originEntry
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
