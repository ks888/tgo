package tracee

import (
	"debug/dwarf"
	"debug/elf"

	"github.com/ks888/tgo/log"
)

var locationListSectionNames = []string{
	".zdebug_loc",
	".debug_loc",
}

func openBinaryFile(pathToProgram string) (BinaryFile, error) {
	elfFile, err := elf.Open(pathToProgram)
	if err != nil {
		return nil, dwarfData{}, err
	}

	data, locList, err := findDWARF(elfFile)
	if err != nil {
		// TODO: try non dwarf version
		closer.Close()
		return nil, err
	}

	binaryFile, err := newDebuggableBinaryFile(dwarfData{Data: data, locationList: locList}, closer)
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

	var locList []byte
	if locListSection != nil {
		locList, err = locListSection.Data()
		if err != nil {
			log.Debugf("failed to read location list section: %v", err)
		}
	}

	data, err := elfFile.DWARF()
	return elfFile, dwarfData{Data: data, locationList: locList}, err
}
