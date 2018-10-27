package tracee

import (
	"debug/macho"
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

	var locListSectionReader io.ReadSeeker
	if locListSection != nil {
		locListSectionReader = locListSection.Open()
	}

	data, err := machoFile.DWARF()
	return machoFile, dwarfData{Data: data, locationList: locListSectionReader}, err
}
