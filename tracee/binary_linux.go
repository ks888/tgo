package tracee

import (
	"debug/dwarf"
	"debug/elf"
	"io"

	"github.com/ks888/tgo/log"
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
		binaryFile, err := newNonDebuggableBinaryFile(findSymbols(elfFile), findFirstModuleData(elfFile), closer)
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

	if locListSection != nil {
		locList, err = locListSection.Data()
		if err != nil {
			log.Debugf("failed to read location list section: %v", err)
		}
	}

	data, err = elfFile.DWARF()
	return data, locList, err
}

func findSymbols(elfFile *elf.File) (symbols []symbol) {
	elfSymbols, err := elfFile.Symbols()
	if err != nil {
		return nil
	}

	for _, sym := range elfSymbols {
		symbols = append(symbols, symbol{Name: sym.Name, Value: sym.Value})
	}
	return symbols
}

func findFirstModuleData(elfFile *elf.File) uint64 {
	symbols, err := elfFile.Symbols()
	if err != nil {
		return 0
	}

	for _, sym := range symbols {
		if sym.Name == firstModuleDataName {
			return sym.Value
		}
	}
	return 0
}
