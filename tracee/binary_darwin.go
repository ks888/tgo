package tracee

import (
	"debug/dwarf"
	"debug/macho"
	"io"
)

func findDWARF(pathToProgram string) (io.Closer, *dwarf.Data, error) {
	machoFile, err := macho.Open(pathToProgram)
	if err != nil {
		return nil, nil, err
	}

	data, err := machoFile.DWARF()
	return machoFile, data, err
}
