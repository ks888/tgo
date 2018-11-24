package tracee

import (
	"debug/dwarf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/ks888/tgo/log"
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
	dwarf        dwarfData
	closer       io.Closer
	GoVersion    GoVersion
	types        map[uint64]dwarf.Offset
	runtimeGType dwarf.Type
}

type dwarfData struct {
	*dwarf.Data
	locationList []byte
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
	// Offset is the offset from the beginning of the parameter list.
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

	binary.GoVersion = binary.findGoVersion()
	binary.types, err = binary.buildTypes()
	if err != nil {
		return Binary{}, err
	}
	binary.runtimeGType, err = binary.findRuntimeGType()
	if err != nil {
		return Binary{}, err
	}
	return binary, nil
}

func (b Binary) buildTypes() (map[uint64]dwarf.Offset, error) {
	if !b.GoVersion.LaterThan(GoVersion{MajorVersion: 1, MinorVersion: 11, PatchVersion: 0}) {
		// attrGoRuntimeType is not supported
		return nil, nil
	}
	types := make(map[uint64]dwarf.Offset)
	reader := b.dwarf.Reader()
	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			return types, err
		}

		switch entry.Tag {
		case dwarf.TagArrayType, dwarf.TagPointerType, dwarf.TagStructType, dwarf.TagSubroutineType, dwarf.TagBaseType, dwarf.TagTypedef:
			// based on the 'abbrevs' variable in src/cmd/internal/dwarf/dwarf.go. It indicates which tag types *may* have the DW_AT_go_runtime_type attribute.
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
			log.Debugf("failed to find the go version of the binary: %v", err)
			return GoVersion{}
		}

		if entry.Tag != dwarf.TagCompileUnit {
			reader.SkipChildren()
			continue
		}

		producer, err := stringClassAttr(entry, dwarf.AttrProducer)
		if err != nil || !strings.HasPrefix(producer, producerPrefix) {
			reader.SkipChildren()
			continue
		}

		return ParseGoVersion(strings.TrimPrefix(producer, producerPrefix))
	}
}

const firstModuleDataName = "runtime.firstmoduledata"

func (b Binary) findFirstModuleDataAddress() (uint64, error) {
	entry, err := b.findDWARFEntryByName(func(entry *dwarf.Entry) bool {
		name, err := stringClassAttr(entry, dwarf.AttrName)
		return name == firstModuleDataName && err == nil
	})
	if err != nil {
		return 0, err
	}

	loc, err := locationClassAttr(entry, dwarf.AttrLocation)
	if err != nil {
		return 0, err
	}
	if len(loc) == 0 || loc[0] != 0x3 {
		return 0, fmt.Errorf("unexpected location format: %v", loc)
	}
	return binary.LittleEndian.Uint64(loc[1:]), nil
}

const gTypeName = "runtime.g"

func (b Binary) findRuntimeGType() (dwarf.Type, error) {
	entry, err := b.findDWARFEntryByName(func(entry *dwarf.Entry) bool {
		if entry.Tag != dwarf.TagStructType {
			return false
		}
		name, err := stringClassAttr(entry, dwarf.AttrName)
		return name == gTypeName && err == nil
	})
	if err != nil {
		return nil, err
	}

	return b.dwarf.Type(entry.Offset)
}

func (b Binary) findDWARFEntryByName(match func(*dwarf.Entry) bool) (*dwarf.Entry, error) {
	reader := b.dwarf.Reader()
	for {
		entry, err := reader.Next()
		if err != nil {
			return nil, err
		} else if entry == nil {
			return nil, errors.New("failed to find a matched entry")
		}

		if match(entry) {
			return entry, nil
		}
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
	dwarfData dwarfData
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
		return function, err
	}
}

func (r subprogramReader) includesPC(subprogram *dwarf.Entry, pc uint64) bool {
	lowPC, err := addressClassAttr(subprogram, dwarf.AttrLowpc)
	if err != nil {
		// inlined subprogram doesn't have the lowPC and highPC attributes.
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
	err := walkUpOrigins(subprogram, r.dwarfData.Data, func(entry *dwarf.Entry) bool {
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
			// the parameters are sorted by the name.
			sort.Slice(params, func(i, j int) bool { return params[i].Offset < params[j].Offset })
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
	err := walkUpOrigins(param, r.dwarfData.Data, func(entry *dwarf.Entry) bool {
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

	offset, exist, err := r.findLocation(param)
	return &Parameter{Name: name, Typ: typ, Offset: offset, IsOutput: isOutput, Exist: exist}, err
}

func (r subprogramReader) findLocation(param *dwarf.Entry) (offset int, exist bool, err error) {
	offset, exist, err = r.findLocationByLocationDesc(param)
	if err != nil && r.dwarfData.locationList != nil {
		offset, exist, err = r.findLocationByLocationList(param)
	}
	return
}

func (r subprogramReader) findLocationByLocationDesc(param *dwarf.Entry) (offset int, exist bool, err error) {
	loc, err := locationClassAttr(param, dwarf.AttrLocation)
	if err != nil {
		return 0, false, fmt.Errorf("loc attr not found: %v", err)
	}

	if len(loc) == 0 {
		// the location description may be empty due to the optimization (see the DWARF spec 2.6.1.1.4)
		return 0, false, nil
	}

	offset, err = parseLocationDesc(loc)
	if err != nil {
		log.Debugf("failed to parse location description at %#x: %v", param.Offset, err)
	}
	return offset, err == nil, nil
}

// parseLocationDesc returns the offset from the beginning of the parameter list.
// It assumes the value is present in the memory and not separated.
// Also, it's supposed the function's frame base always specifies to the CFA.
func parseLocationDesc(loc []byte) (int, error) {
	if len(loc) == 0 {
		return 0, errors.New("location description is empty")
	}

	// TODO: support the value in the register and the separated value.
	switch loc[0] {
	case dwarfOpCallFrameCFA:
		return 0, nil
	case dwarfOpFbreg:
		return decodeSignedLEB128(loc[1:]), nil
	default:
		return 0, fmt.Errorf("unknown operation: %#x", loc[0])
	}
}

func (r subprogramReader) findLocationByLocationList(param *dwarf.Entry) (int, bool, error) {
	loc, err := locationListClassAttr(param, dwarf.AttrLocation)
	if err != nil {
		return 0, false, fmt.Errorf("loc list attr not found: %v", err)
	}

	locList := buildLocationList(r.dwarfData.locationList, int(loc))
	if len(locList.locListEntries) == 0 {
		return 0, false, errors.New("no location list entry")
	}

	// TODO: it's more precise to choose the right location list entry using PC and address offsets.
	//       Usually the first entry specifies to the right location in our use case, though.
	offset, err := parseLocationDesc(locList.locListEntries[0].locationDesc)
	if err != nil {
		log.Debugf("failed to parse location list at %#x: %v", param.Offset, err)
	}
	return offset, err == nil, nil
}

type locationList struct {
	baseAddress    uint64
	locListEntries []locationListEntry
}

type locationListEntry struct {
	beginOffset, endOffset int
	locationDesc           []byte
}

func buildLocationList(locSectionData []byte, offset int) (locList locationList) {
	for {
		beginOffset := binary.LittleEndian.Uint64(locSectionData[offset : offset+8])
		offset += 8
		endOffset := binary.LittleEndian.Uint64(locSectionData[offset : offset+8])
		offset += 8
		if beginOffset == 0x0 && endOffset == 0x0 {
			// end of list entry
			break
		} else if beginOffset == ^uint64(0) {
			// base address selection entry
			locList.baseAddress = endOffset
			continue
		}

		// location list entry
		locListEntry := locationListEntry{beginOffset: int(beginOffset), endOffset: int(endOffset)}
		locationDescLen := int(binary.LittleEndian.Uint16(locSectionData[offset : offset+2]))
		offset += 2

		locListEntry.locationDesc = locSectionData[offset : offset+locationDescLen]
		offset += locationDescLen

		locList.locListEntries = append(locList.locListEntries, locListEntry)
	}
	return
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
