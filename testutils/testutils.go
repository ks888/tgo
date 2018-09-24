package testutils

import (
	"debug/elf"
	"debug/macho"
	"fmt"
	"os/exec"
	"path"
	"runtime"
)

var (
	ProgramParameters                     string
	ProgramParametersNoDwarf              string
	ParametersAddrMain                    uint64
	ParametersAddrNoParameter             uint64
	ParametersAddrOneParameter            uint64
	ParametersAddrOneParameterAndVariable uint64
	ParametersAddrTwoParameters           uint64
	ParametersAddrFuncWithAbstractOrigin  uint64 // any function which corresponding DIE has the DW_AT_abstract_origin attribute.

	ProgramInfloop    string
	InfloopAddrMain   uint64
	InfloopEntrypoint uint64
)

func init() {
	_, srcFilename, _, _ := runtime.Caller(0)
	srcDirname := path.Dir(srcFilename)
	ProgramInfloop = srcDirname + "/testdata/infloop"

	if err := buildProgramParameters(srcDirname); err != nil {
		panic(err)
	}
	if err := buildProgramInfloop(srcDirname); err != nil {
		panic(err)
	}
}

func buildProgramParameters(srcDirname string) error {
	ProgramParameters = srcDirname + "/testdata/parameters"
	ProgramParametersNoDwarf = ProgramParameters + ".nodwarf"

	src := ProgramParameters + ".go"
	if out, err := exec.Command("go", "build", "-o", ProgramParameters, src).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build %s: %v\n%v", src, err, string(out))
	}

	if out, err := exec.Command("go", "build", "-ldflags", "-w", "-o", ProgramParametersNoDwarf, src).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build %s: %v\n%v", src, err, string(out))
	}

	updateAddressIfMatched := func(name string, value uint64) {
		switch name {
		case "main.main":
			ParametersAddrMain = value
		case "main.oneParameter":
			ParametersAddrOneParameter = value
		case "main.oneParameterAndOneVariable":
			ParametersAddrOneParameterAndVariable = value
		case "main.noParameter":
			ParametersAddrNoParameter = value
		case "main.twoParameters":
			ParametersAddrTwoParameters = value
		case "reflect.Value.Kind":
			ParametersAddrFuncWithAbstractOrigin = value
		}
	}

	switch runtime.GOOS {
	case "darwin":
		machoFile, err := macho.Open(ProgramParameters)
		if err != nil {
			return fmt.Errorf("failed to open binary: %v", err)
		}
		for _, sym := range machoFile.Symtab.Syms {
			updateAddressIfMatched(sym.Name, sym.Value)
		}

	case "linux":
		elfFile, err := elf.Open(ProgramParameters)
		if err != nil {
			return fmt.Errorf("failed to open binary: %v", err)
		}
		syms, err := elfFile.Symbols()
		if err != nil {
			return fmt.Errorf("failed to find symbols: %v", err)
		}
		for _, sym := range syms {
			updateAddressIfMatched(sym.Name, sym.Value)
		}
	default:
		return fmt.Errorf("unsupported os: %s", runtime.GOOS)
	}

	return nil
}

func buildProgramInfloop(srcDirname string) error {
	ProgramInfloop = srcDirname + "/testdata/infloop"

	src := ProgramInfloop + ".go"
	if out, err := exec.Command("go", "build", "-o", ProgramInfloop, src).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build %s: %v\n%v", src, err, string(out))
	}

	updateAddressIfMatched := func(name string, value uint64) {
		switch name {
		case "main.main":
			InfloopAddrMain = value
		}
	}

	switch runtime.GOOS {
	case "darwin":
		machoFile, err := macho.Open(ProgramInfloop)
		if err != nil {
			return fmt.Errorf("failed to open binary: %v", err)
		}
		for _, sym := range machoFile.Symtab.Syms {
			updateAddressIfMatched(sym.Name, sym.Value)
		}

	case "linux":
		elfFile, err := elf.Open(ProgramInfloop)
		if err != nil {
			return fmt.Errorf("failed to open binary: %v", err)
		}

		InfloopEntrypoint = elfFile.Entry

		syms, err := elfFile.Symbols()
		if err != nil {
			return fmt.Errorf("failed to find symbols: %v", err)
		}
		for _, sym := range syms {
			updateAddressIfMatched(sym.Name, sym.Value)
		}
	default:
		return fmt.Errorf("unsupported os: %s", runtime.GOOS)
	}

	return nil
}
