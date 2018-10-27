package tracee

import (
	"bytes"
	"compress/zlib"
	"debug/macho"
	"encoding/binary"
	"io"
)

var locationListSectionNames = []string{
	"__zdebug_loc",
	"__debug_loc",
}

func findDWARF(pathToProgram string) (io.Closer, dwarfData, error) {
	machoFile, err := macho.Open(pathToProgram)
	if err != nil {
		return nil, dwarfData{}, err
	}

	var locListSection *macho.Section
	for _, locListSectionName := range locationListSectionNames {
		locListSection = machoFile.Section(locListSectionName)
		if locListSection != nil {
			break
		}
	}
	// older go version doesn't create a location list section.

	locList, err := buildLocationListData(locListSection)
	if err != nil {
		return nil, dwarfData{}, err
	}

	data, err := machoFile.DWARF()
	return machoFile, dwarfData{Data: data, locationList: locList}, err
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
