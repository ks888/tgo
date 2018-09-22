package tracee

import (
	"debug/dwarf"
	"debug/elf"
	"io"
)

func findDWARF(pathToProgram string) (io.Closer, *dwarf.Data, error) {
	elfFile, err := elf.Open(pathToProgram)
	if err != nil {
		return nil, nil, err
	}

	data, err := elfFile.DWARF()
	return elfFile, data, err
}
