package tracee

import (
	"debug/elf"
	"io"

	"github.com/ks888/tgo/log"
)

var locationListSectionNames = []string{
	".zdebug_loc",
	".debug_loc",
}

func findDWARF(pathToProgram string) (io.Closer, dwarfData, error) {
	elfFile, err := elf.Open(pathToProgram)
	if err != nil {
		return nil, dwarfData{}, err
	}

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
