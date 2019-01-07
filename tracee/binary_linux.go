package tracee

import (
	"bytes"
	"compress/zlib"
	"debug/dwarf"
	"debug/elf"
	"encoding/binary"
	"io"
)

var locationListSectionNames = []string{
	".zdebug_loc",
	".debug_loc",
}

func openBinaryFile(pathToProgram string, goVersion GoVersion) (BinaryFile, error) {
	elfFile, err := elf.Open(pathToProgram)
	if err != nil {
		return nil, err
	}
	var closer io.Closer = elfFile

	data, locList, err := findDWARF(elfFile)
	if err != nil {
		binaryFile, err := newNonDebuggableBinaryFile(closer)
		if err != nil {
			closer.Close()
		}
		return binaryFile, err
	}

	binaryFile, err := newDebuggableBinaryFile(dwarfData{Data: data, locationList: locList}, goVersion, closer)
	if err != nil {
		closer.Close()
	}
	return binaryFile, err
}

func findDWARF(elfFile *elf.File) (data *dwarf.Data, locList []byte, err error) {
	var locListSection *elf.Section
	for _, locListSectionName := range locationListSectionNames {
		locListSection = elfFile.Section(locListSectionName)
		if locListSection != nil {
			break
		}
	}
	// older go version doesn't create a location list section.

	locList, err = buildLocationListData(locListSection)
	if err != nil {
		return nil, nil, err
	}

	data, err = elfFile.DWARF()
	return data, locList, err
}

func buildLocationListData(locListSection *elf.Section) ([]byte, error) {
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
