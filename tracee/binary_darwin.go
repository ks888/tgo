package tracee

import (
	"debug/dwarf"
	"debug/macho"
)

func findDWARF(pathToProgram string) (*dwarf.Data, error) {
	machoFile, err := macho.Open(pathToProgram)
	if err != nil {
		return nil, err
	}

	return machoFile.DWARF()
}
