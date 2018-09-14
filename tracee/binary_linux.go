package tracee

import (
	"debug/dwarf"
	"debug/elf"
)

func findDWARF(pathToProgram string) (*dwarf.Data, error) {
	elfFile, err := elf.Open(pathToProgram)
	if err != nil {
		return nil, err
	}

	return elfFile.DWARF()
}
