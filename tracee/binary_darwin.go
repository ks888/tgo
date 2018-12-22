package tracee

import (
	"bytes"
	"compress/zlib"
	"debug/dwarf"
	"debug/macho"
	"encoding/binary"
	"io"
)

var locationListSectionNames = []string{
	"__zdebug_loc",
	"__debug_loc",
}

func openBinaryFile(pathToProgram string) (BinaryFile, error) {
	machoFile, err := macho.Open(pathToProgram)
	if err != nil {
		return nil, err
	}
	var closer io.Closer = machoFile

	data, locList, err := findDWARF(machoFile)
	if err != nil {
		binaryFile, err := newNonDebuggableBinaryFile(findSymbols(machoFile), closer)
		if err != nil {
			closer.Close()
		}
		return binaryFile, err
	}

	binaryFile, err := newDebuggableBinaryFile(dwarfData{Data: data, locationList: locList}, closer)
	if err != nil {
		closer.Close()
	}
	return binaryFile, err
}

func findDWARF(machoFile *macho.File) (data *dwarf.Data, locList []byte, err error) {
	var locListSection *macho.Section
	for _, locListSectionName := range locationListSectionNames {
		locListSection = machoFile.Section(locListSectionName)
		if locListSection != nil {
			break
		}
	}
	// older go version doesn't create a location list section.

	locList, err = buildLocationListData(locListSection)
	if err != nil {
		return nil, nil, err
	}

	data, err = machoFile.DWARF()
	return data, locList, err
}

func buildLocationListData(locListSection *macho.Section) ([]byte, error) {
	if locListSection == nil {
		return nil, nil
	}

	rawData, err := locListSection.Data()
	if err != nil {
		return nil, err
	}

	if string(rawData[:4]) != "ZLIB" || len(rawData) < 12 {
		return rawData, nil
	}

	dlen := binary.BigEndian.Uint64(rawData[4:12])
	uncompressedData := make([]byte, dlen)

	r, err := zlib.NewReader(bytes.NewBuffer(rawData[12:]))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	_, err = io.ReadFull(r, uncompressedData)
	return uncompressedData, err
}

func findSymbols(machoFile *macho.File) (symbols []symbol) {
	if machoFile.Symtab == nil {
		return
	}

	for _, sym := range machoFile.Symtab.Syms {
		symbols = append(symbols, symbol{Name: sym.Name, Value: sym.Value})
	}
	return symbols
}
