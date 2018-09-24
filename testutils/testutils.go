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
	ProgramHelloworld                     string
	ProgramHelloworldNoDwarf              string
	HelloworldAddrMain                    uint64
	HelloworldAddrNoParameter             uint64
	HelloworldAddrOneParameter            uint64
	HelloworldAddrOneParameterAndVariable uint64
	HelloworldAddrTwoParameters           uint64
	HelloworldAddrFuncWithAbstractOrigin  uint64 // any function which corresponding DIE has the DW_AT_abstract_origin attribute.

	ProgramInfloop    string
	InfloopAddrMain   uint64
	InfloopEntrypoint uint64
)

func init() {
	_, srcFilename, _, _ := runtime.Caller(0)
	srcDirname := path.Dir(srcFilename)
	ProgramInfloop = srcDirname + "/testdata/infloop"

	if err := buildProgramHelloworld(srcDirname); err != nil {
		panic(err)
	}
	if err := buildProgramInfloop(srcDirname); err != nil {
		panic(err)
	}
}

func buildProgramHelloworld(srcDirname string) error {
	ProgramHelloworld = srcDirname + "/testdata/helloworld"
	ProgramHelloworldNoDwarf = ProgramHelloworld + ".nodwarf"

	src := ProgramHelloworld + ".go"
	if out, err := exec.Command("go", "build", "-o", ProgramHelloworld, src).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build %s: %v\n%v", src, err, string(out))
	}

	if out, err := exec.Command("go", "build", "-ldflags", "-w", "-o", ProgramHelloworldNoDwarf, src).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build %s: %v\n%v", src, err, string(out))
	}

	updateAddressIfMatched := func(name string, value uint64) {
		switch name {
		case "main.main":
			HelloworldAddrMain = value
		case "main.oneParameter":
			HelloworldAddrOneParameter = value
		case "main.oneParameterAndOneVariable":
			HelloworldAddrOneParameterAndVariable = value
		case "main.noParameter":
			HelloworldAddrNoParameter = value
		case "main.twoParameters":
			HelloworldAddrTwoParameters = value
		case "reflect.Value.Kind":
			HelloworldAddrFuncWithAbstractOrigin = value
		}
	}

	switch runtime.GOOS {
	case "darwin":
		machoFile, err := macho.Open(ProgramHelloworld)
		if err != nil {
			return fmt.Errorf("failed to open binary: %v", err)
		}
		for _, sym := range machoFile.Symtab.Syms {
			updateAddressIfMatched(sym.Name, sym.Value)
		}

	case "linux":
		elfFile, err := elf.Open(ProgramHelloworld)
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
