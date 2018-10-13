package testutils

import (
	"debug/elf"
	"debug/macho"
	"fmt"
	"os/exec"
	"path"
	"runtime"
	"strings"
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
	HelloworldAddrFmtPrintln              uint64

	ProgramInfloop  string
	InfloopAddrMain uint64

	ProgramGoRoutines  string
	GoRoutinesAddrMain uint64

	ProgramRecursive  string
	RecursiveAddrMain uint64

	ProgramPanic           string
	PanicAddrMain          uint64
	PanicAddrThrow         uint64
	PanicAddrInsideThrough uint64
	PanicAddrCatch         uint64
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
	if err := buildProgramGoRoutines(srcDirname); err != nil {
		panic(err)
	}
	if err := buildProgramRecursive(srcDirname); err != nil {
		panic(err)
	}
	if err := buildProgramPanic(srcDirname); err != nil {
		panic(err)
	}
}

func buildProgramHelloworld(srcDirname string) error {
	ProgramHelloworld = srcDirname + "/testdata/helloworld"
	if err := buildProgram(ProgramHelloworld); err != nil {
		return err
	}

	src := ProgramHelloworld + ".go"
	ProgramHelloworldNoDwarf = ProgramHelloworld + ".nodwarf"
	if out, err := exec.Command("go", "build", "-ldflags", "-w", "-o", ProgramHelloworldNoDwarf, src).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build %s: %v\n%v", src, err, string(out))
	}

	updateAddressIfMatched := func(name string, value uint64) error {
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
		case "fmt.Println":
			HelloworldAddrFmtPrintln = value
		case "reflect.Value.Kind":
			HelloworldAddrFuncWithAbstractOrigin = value
		}
		return nil
	}

	return walkSymbols(ProgramHelloworld, updateAddressIfMatched)
}

func buildProgramInfloop(srcDirname string) error {
	ProgramInfloop = srcDirname + "/testdata/infloop"

	if err := buildProgram(ProgramInfloop); err != nil {
		return err
	}

	updateAddressIfMatched := func(name string, value uint64) error {
		switch name {
		case "main.main":
			InfloopAddrMain = value
		}
		return nil
	}

	return walkSymbols(ProgramInfloop, updateAddressIfMatched)
}

func buildProgramGoRoutines(srcDirname string) error {
	ProgramGoRoutines = srcDirname + "/testdata/goroutines"

	if err := buildProgram(ProgramGoRoutines); err != nil {
		return err
	}

	updateAddressIfMatched := func(name string, value uint64) error {
		switch name {
		case "main.main":
			GoRoutinesAddrMain = value
		}
		return nil
	}

	return walkSymbols(ProgramGoRoutines, updateAddressIfMatched)
}

func buildProgramRecursive(srcDirname string) error {
	ProgramRecursive = srcDirname + "/testdata/recursive"

	if err := buildProgram(ProgramRecursive); err != nil {
		return err
	}

	updateAddressIfMatched := func(name string, value uint64) error {
		switch name {
		case "main.main":
			RecursiveAddrMain = value
		}
		return nil
	}

	return walkSymbols(ProgramRecursive, updateAddressIfMatched)
}

func buildProgramPanic(srcDirname string) error {
	ProgramPanic = srcDirname + "/testdata/panic"

	if err := buildProgram(ProgramPanic); err != nil {
		return err
	}

	updateAddressIfMatched := func(name string, value uint64) error {
		switch name {
		case "main.main":
			PanicAddrMain = value
		case "main.throw":
			PanicAddrThrow = value
		case "main.through.func1":
			PanicAddrInsideThrough = value
		case "main.catch":
			PanicAddrCatch = value
		}
		return nil
	}

	return walkSymbols(ProgramPanic, updateAddressIfMatched)
}

func buildProgram(programName string) error {
	const compileOptions = "all=-N -l" // to prevent function inlining.
	linkOptions := ""
	if strings.HasPrefix(runtime.Version(), "go1.11") {
		linkOptions = "-compressdwarf=false" // not required, but useful for debugging.
	}
	src := programName + ".go"
	if out, err := exec.Command("go", "build", "-gcflags", compileOptions, "-ldflags", linkOptions, "-o", programName, src).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build %s: %v\n%v", src, err, string(out))
	}
	return nil
}

func walkSymbols(programName string, walkFunc func(name string, value uint64) error) error {
	switch runtime.GOOS {
	case "darwin":
		machoFile, err := macho.Open(programName)
		if err != nil {
			return fmt.Errorf("failed to open binary: %v", err)
		}
		for _, sym := range machoFile.Symtab.Syms {
			if err := walkFunc(sym.Name, sym.Value); err != nil {
				return err
			}
		}

	case "linux":
		elfFile, err := elf.Open(programName)
		if err != nil {
			return fmt.Errorf("failed to open binary: %v", err)
		}

		syms, err := elfFile.Symbols()
		if err != nil {
			return fmt.Errorf("failed to find symbols: %v", err)
		}
		for _, sym := range syms {
			if err := walkFunc(sym.Name, sym.Value); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported os: %s", runtime.GOOS)
	}

	return nil
}
