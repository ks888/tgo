package tracee

import (
	"debug/dwarf"
	"debug/elf"
)

// Binary represents the program the tracee process executes
type Binary struct {
	dwarfData *dwarf.Data
}

// NewBinary returns the new binary object associated to the program.
func NewBinary(pathToProgram string) (*Binary, error) {
	elfFile, err := elf.Open(pathToProgram)
	if err != nil {
		return nil, err
	}

	dwarfData, err := elfFile.DWARF()
	if err != nil {
		return nil, err
	}

	return &Binary{dwarfData: dwarfData}, nil
}
