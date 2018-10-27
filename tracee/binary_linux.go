package tracee

import (
	"debug/elf"
	"io"
)

func findDWARF(pathToProgram string) (io.Closer, dwarfData, error) {
	elfFile, err := elf.Open(pathToProgram)
	if err != nil {
		return nil, dwarfData{}, err
	}

	// TODO: find loc list

	data, err := elfFile.DWARF()
	return elfFile, dwarfData{Data: data}, err
}
