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

// BinaryFile represents the program the tracee process is executing.
type BinaryFile interface {
	// FindFunction returns the function info to which the given pc specifies.
	FindFunction(pc uint64) (*Function, error)
	// Functions returns all the functions defined in the binary.
	Functions() []*Function
	// Close closes the binary file.
	Close() error
	// findDwarfTypeByAddr finds the dwarf.Type to which the given address specifies.
	// The given address must be the address of the type (not value) and need to be adjusted
	// using the moduledata.
	findDwarfTypeByAddr(typeAddr uint64) (dwarf.Type, error)
	// firstModuleDataAddress returns the address of runtime.firstmoduledata.
	firstModuleDataAddress() uint64
	// moduleDataType returns the dwarf.Type of runtime.moduledata struct type.
	moduleDataType() dwarf.Type
	// runtimeGType returns the dwarf.Type of runtime.g struct type.
	runtimeGType() dwarf.Type
}

// debuggableBinaryFile represents the binary file with DWARF sections.
type debuggableBinaryFile struct {
	functions                    []*Function
	dwarf                        dwarfData
	closer                       io.Closer
	types                        map[uint64]dwarf.Offset
	cachedRuntimeGType           dwarf.Type
	cachedFirstModuleDataAddress uint64
	cachedModuleDataType         dwarf.Type
}

type dwarfData struct {
	*dwarf.Data
	locationList []byte
}

// Function represents a function info in the debug info section.
type Function struct {
	Name string
	// StartAddr is the start address of the function, inclusive.
	StartAddr uint64
	// EndAddr is the end address of the function, exclusive. 0 if unknown.
	EndAddr uint64
	// Parameters may be empty due to the lack of information.
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

// OpenBinaryFile opens the specified program file.
func OpenBinaryFile(pathToProgram string, goVersion GoVersion) (BinaryFile, error) {
	return openBinaryFile(pathToProgram, goVersion)
}

func newDebuggableBinaryFile(data dwarfData, goVersion GoVersion, closer io.Closer) (debuggableBinaryFile, error) {
	binary := debuggableBinaryFile{dwarf: data, closer: closer}

	var err error
	binary.functions, err = binary.listFunctions()
	if err != nil {
		return debuggableBinaryFile{}, err
	}

	binary.types, err = binary.buildTypes(goVersion)
	if err != nil {
		return debuggableBinaryFile{}, err
	}

	binary.cachedFirstModuleDataAddress, err = binary.findFirstModuleDataAddress()
	if err != nil {
		return debuggableBinaryFile{}, err
	}

	binary.cachedModuleDataType, err = binary.findModuleDataType()
	if err != nil {
		return debuggableBinaryFile{}, err
	}

	binary.cachedRuntimeGType, err = binary.findRuntimeGType()
	if err != nil {
		return debuggableBinaryFile{}, err
	}

	return binary, nil
}

func (b debuggableBinaryFile) listFunctions() ([]*Function, error) {
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

func (b debuggableBinaryFile) buildTypes(goVersion GoVersion) (map[uint64]dwarf.Offset, error) {
	if !goVersion.LaterThan(GoVersion{MajorVersion: 1, MinorVersion: 11, PatchVersion: 0}) {
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

const firstModuleDataName = "runtime.firstmoduledata"

func (b debuggableBinaryFile) findFirstModuleDataAddress() (uint64, error) {
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

const moduleDataTypeName = "runtime.moduledata"

func (b debuggableBinaryFile) findModuleDataType() (dwarf.Type, error) {
	return b.findType(dwarf.TagStructType, moduleDataTypeName)
}

const gTypeName = "runtime.g"

func (b debuggableBinaryFile) findRuntimeGType() (dwarf.Type, error) {
	return b.findType(dwarf.TagStructType, gTypeName)
}

func (b debuggableBinaryFile) findType(targetTag dwarf.Tag, targetName string) (dwarf.Type, error) {
	entry, err := b.findDWARFEntryByName(func(entry *dwarf.Entry) bool {
		if entry.Tag != targetTag {
			return false
		}
		name, err := stringClassAttr(entry, dwarf.AttrName)
		return name == targetName && err == nil
	})
	if err != nil {
		return nil, err
	}

	return b.dwarf.Type(entry.Offset)
}

func (b debuggableBinaryFile) findDWARFEntryByName(match func(*dwarf.Entry) bool) (*dwarf.Entry, error) {
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

// FindFunction looks up the function info described in the debug info section.
func (b debuggableBinaryFile) FindFunction(pc uint64) (*Function, error) {
	reader := subprogramReader{raw: b.dwarf.Reader(), dwarfData: b.dwarf}
	return reader.Seek(pc)
}

// Functions lists the subprograms in the debug info section. They don't include parameters info.
func (b debuggableBinaryFile) Functions() []*Function {
	return b.functions
}

// Close releases the resources associated with the binary.
func (b debuggableBinaryFile) Close() error {
	return b.closer.Close()
}

func (b debuggableBinaryFile) findDwarfTypeByAddr(typeAddr uint64) (dwarf.Type, error) {
	implTypOffset := b.types[typeAddr]
	return b.dwarf.Type(implTypOffset)
}

func (b debuggableBinaryFile) firstModuleDataAddress() uint64 {
	return b.cachedFirstModuleDataAddress
}

func (b debuggableBinaryFile) moduleDataType() dwarf.Type {
	return b.cachedModuleDataType
}

func (b debuggableBinaryFile) runtimeGType() dwarf.Type {
	return b.cachedRuntimeGType
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

	highPC, err := addressClassAttr(subprogram, dwarf.AttrHighpc)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	}

	frameBase, err := locationClassAttr(subprogram, dwarf.AttrFrameBase)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", name, err)
	} else if len(frameBase) != 1 || frameBase[0] != dwarfOpCallFrameCFA {
		log.Printf("The frame base attribute of %s has the unexpected value. The parameter values may be wrong.", name)
	}

	return &Function{Name: name, StartAddr: lowPC, EndAddr: highPC}, nil
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

type symbol struct {
	Name  string
	Value uint64
}

// nonDebuggableBinaryFile represents the binary file WITHOUT DWARF sections.
type nonDebuggableBinaryFile struct {
	closer              io.Closer
	symbols             []symbol
	firstModuleDataAddr uint64
}

func newNonDebuggableBinaryFile(symbols []symbol, firstModuleDataAddr uint64, closer io.Closer) (nonDebuggableBinaryFile, error) {
	return nonDebuggableBinaryFile{closer: closer, firstModuleDataAddr: firstModuleDataAddr, symbols: symbols}, nil
}

// FindFunction always returns error because it's difficult to get function info using non-DWARF binary.
func (b nonDebuggableBinaryFile) FindFunction(pc uint64) (*Function, error) {
	return nil, errors.New("no DWARF info")
}

func (b nonDebuggableBinaryFile) Functions() (funcs []*Function) {
	for _, sym := range b.symbols {
		funcs = append(funcs, &Function{Name: sym.Name, StartAddr: sym.Value})
	}
	return funcs
}

func (b nonDebuggableBinaryFile) Close() error {
	return b.closer.Close()
}

func (b nonDebuggableBinaryFile) findDwarfTypeByAddr(typeAddr uint64) (dwarf.Type, error) {
	return nil, errors.New("no DWARF info")
}

func (b nonDebuggableBinaryFile) firstModuleDataAddress() uint64 {
	return b.firstModuleDataAddr
}

// Assume this dwarf.Type represents a subset of the module data type in the case DWARF is not available.
var moduleDataType = &dwarf.StructType{
	StructName: "runtime.moduledata",
	CommonType: dwarf.CommonType{ByteSize: 456},
	Field: []*dwarf.StructField{
		&dwarf.StructField{
			Name: "pclntable",
			Type: &dwarf.StructType{
				CommonType: dwarf.CommonType{ByteSize: 24},
				StructName: "[]uint8",
				Field: []*dwarf.StructField{
					&dwarf.StructField{
						Name: "array",
						Type: &dwarf.PtrType{
							CommonType: dwarf.CommonType{ByteSize: 8},
							Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 1}}},
						},
						ByteOffset: 0,
					},
					&dwarf.StructField{
						Name:       "len",
						Type:       &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
						ByteOffset: 8,
					},
				},
			},
			ByteOffset: 0,
		},
		&dwarf.StructField{
			Name: "ftab",
			Type: &dwarf.StructType{
				CommonType: dwarf.CommonType{ByteSize: 24},
				StructName: "[]runtime.functab",
				Field: []*dwarf.StructField{
					&dwarf.StructField{
						Name: "array",
						Type: &dwarf.PtrType{
							CommonType: dwarf.CommonType{ByteSize: 8},
							Type: &dwarf.StructType{
								CommonType: dwarf.CommonType{ByteSize: 16},
								StructName: "runtime.functab",
								Field: []*dwarf.StructField{
									&dwarf.StructField{
										Name:       "entry",
										Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
										ByteOffset: 0,
									},
									&dwarf.StructField{
										Name:       "funcoff",
										Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
										ByteOffset: 8,
									},
								},
							},
						},
						ByteOffset: 0,
					},
					&dwarf.StructField{
						Name:       "len",
						Type:       &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
						ByteOffset: 8,
					},
				},
			},
			ByteOffset: 24,
		},
		&dwarf.StructField{
			Name:       "findfunctab",
			Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
			ByteOffset: 72,
		},
		&dwarf.StructField{
			Name:       "minpc",
			Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
			ByteOffset: 80,
		},
		&dwarf.StructField{
			Name:       "maxpc",
			Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
			ByteOffset: 88,
		},
		&dwarf.StructField{
			Name:       "types",
			Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
			ByteOffset: 200,
		},
		&dwarf.StructField{
			Name:       "etypes",
			Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
			ByteOffset: 208,
		},
		&dwarf.StructField{
			Name:       "next",
			Type:       &dwarf.PtrType{CommonType: dwarf.CommonType{ByteSize: 8}},
			ByteOffset: 448,
		},
	},
}

func (b nonDebuggableBinaryFile) moduleDataType() dwarf.Type {
	return moduleDataType
}

// Assume this dwarf.Type represents a subset of the runtime.g type in the case DWARF is not available.
var runtimeGType = &dwarf.StructType{
	StructName: "runtime.moduledata",
	CommonType: dwarf.CommonType{ByteSize: 456},
	Field: []*dwarf.StructField{
		&dwarf.StructField{
			Name: "stack",
			Type: &dwarf.StructType{
				CommonType: dwarf.CommonType{ByteSize: 16},
				StructName: "runtime.stack",
				Field: []*dwarf.StructField{
					&dwarf.StructField{
						Name:       "lo",
						Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
						ByteOffset: 0,
					},
					&dwarf.StructField{
						Name:       "hi",
						Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
						ByteOffset: 8,
					},
				},
			},
			ByteOffset: 0,
		},
		&dwarf.StructField{
			Name:       "_panic",
			Type:       &dwarf.PtrType{CommonType: dwarf.CommonType{ByteSize: 8}},
			ByteOffset: 32,
		},
		&dwarf.StructField{
			Name: "_defer",
			Type: &dwarf.PtrType{
				CommonType: dwarf.CommonType{ByteSize: 8},
				Type: &dwarf.StructType{
					CommonType: dwarf.CommonType{ByteSize: 48},
					StructName: "runtime._defer",
					Field: []*dwarf.StructField{
						&dwarf.StructField{
							Name:       "sp",
							Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
							ByteOffset: 8,
						},
						&dwarf.StructField{
							Name:       "pc",
							Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
							ByteOffset: 16,
						},
						&dwarf.StructField{
							Name:       "_panic",
							Type:       &dwarf.PtrType{CommonType: dwarf.CommonType{ByteSize: 8}},
							ByteOffset: 32,
						},
						&dwarf.StructField{
							Name:       "link",
							Type:       &dwarf.PtrType{CommonType: dwarf.CommonType{ByteSize: 8}},
							ByteOffset: 40,
						},
					},
				},
			},
			ByteOffset: 40,
		},
		&dwarf.StructField{
			Name:       "goid",
			Type:       &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
			ByteOffset: 152,
		},
		&dwarf.StructField{
			Name: "ancestors",
			Type: &dwarf.PtrType{
				CommonType: dwarf.CommonType{ByteSize: 8},
				Type: &dwarf.StructType{
					CommonType: dwarf.CommonType{ByteSize: 24},
					StructName: "[]runtime.ancestorInfo",
					Field: []*dwarf.StructField{
						&dwarf.StructField{
							Name: "array",
							Type: &dwarf.PtrType{
								CommonType: dwarf.CommonType{ByteSize: 8},
								Type: &dwarf.StructType{
									CommonType: dwarf.CommonType{ByteSize: 40},
									StructName: "runtime.ancestorInfo",
									Field: []*dwarf.StructField{
										&dwarf.StructField{
											Name:       "goid",
											Type:       &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
											ByteOffset: 24,
										},
									},
								},
							},
							ByteOffset: 0,
						},
						&dwarf.StructField{
							Name:       "len",
							Type:       &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
							ByteOffset: 8,
						},
					},
				},
			},
			ByteOffset: 288,
		},
	},
}

func (b nonDebuggableBinaryFile) runtimeGType() dwarf.Type {
	return runtimeGType
}
