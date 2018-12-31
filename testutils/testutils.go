package testutils

import (
	"debug/elf"
	"debug/macho"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ks888/tgo/log"
)

var (
	// goBinaryPath is the path to the go binary used to build this test program.
	// This go binary is used to build the testdata.
	goBinaryPath string = filepath.Join(runtime.GOROOT(), "bin", "go")

	ProgramHelloworld        string
	ProgramHelloworldNoDwarf string
	// These addresses are retrieved from the dwarf version. Assume they are same as non-dwarf version.
	HelloworldAddrMain                    uint64
	HelloworldAddrNoParameter             uint64
	HelloworldAddrOneParameter            uint64
	HelloworldAddrOneParameterAndVariable uint64
	HelloworldAddrTwoParameters           uint64
	HelloworldAddrFuncWithAbstractOrigin  uint64 // any function which corresponding DIE has the DW_AT_abstract_origin attribute.
	HelloworldAddrFmtPrintln              uint64
	HelloworldAddrGoBuildID               uint64

	ProgramInfloop  string
	InfloopAddrMain uint64

	ProgramGoRoutines        string
	ProgramGoRoutinesNoDwarf string
	GoRoutinesAddrMain       uint64
	GoRoutinesAddrInc        uint64

	ProgramRecursive  string
	RecursiveAddrMain uint64

	ProgramPanic           string
	ProgramPanicNoDwarf    string
	PanicAddrMain          uint64
	PanicAddrThrow         uint64
	PanicAddrInsideThrough uint64
	PanicAddrCatch         uint64

	ProgramTypePrint                    string
	TypePrintAddrPrintBool              uint64
	TypePrintAddrPrintInt8              uint64
	TypePrintAddrPrintInt16             uint64
	TypePrintAddrPrintInt32             uint64
	TypePrintAddrPrintInt64             uint64
	TypePrintAddrPrintUint8             uint64
	TypePrintAddrPrintUint16            uint64
	TypePrintAddrPrintUint32            uint64
	TypePrintAddrPrintUint64            uint64
	TypePrintAddrPrintFloat32           uint64
	TypePrintAddrPrintFloat64           uint64
	TypePrintAddrPrintComplex64         uint64
	TypePrintAddrPrintComplex128        uint64
	TypePrintAddrPrintString            uint64
	TypePrintAddrPrintArray             uint64
	TypePrintAddrPrintSlice             uint64
	TypePrintAddrPrintNilSlice          uint64
	TypePrintAddrPrintStruct            uint64
	TypePrintAddrPrintPtr               uint64
	TypePrintAddrPrintFunc              uint64
	TypePrintAddrPrintInterface         uint64
	TypePrintAddrPrintPtrInterface      uint64
	TypePrintAddrPrintNilInterface      uint64
	TypePrintAddrPrintEmptyInterface    uint64
	TypePrintAddrPrintNilEmptyInterface uint64
	TypePrintAddrPrintMap               uint64
	TypePrintAddrPrintNilMap            uint64
	TypePrintAddrPrintChan              uint64

	ProgramStartStop        string
	StartStopAddrTracedFunc uint64
	StartStopAddrTracerOff  uint64

	ProgramStartOnly string
)

func init() {
	_, srcFilename, _, _ := runtime.Caller(0)
	srcDirname := filepath.Dir(srcFilename)

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
	if err := buildProgramTypePrint(srcDirname); err != nil {
		panic(err)
	}
	if err := buildProgramStartStop(srcDirname); err != nil {
		panic(err)
	}
	if err := buildProgramStartOnly(srcDirname); err != nil {
		panic(err)
	}

	log.EnableDebugLog = true
}

func buildProgramHelloworld(srcDirname string) error {
	// TODO: use filepath.Join
	ProgramHelloworld = srcDirname + "/testdata/helloworld"
	if err := buildProgram(ProgramHelloworld); err != nil {
		return err
	}

	ProgramHelloworldNoDwarf = ProgramHelloworld + ".nodwarf"
	if err := buildProgramWithoutDWARF(ProgramHelloworld+".go", ProgramHelloworldNoDwarf); err != nil {
		return err
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
		case "go.buildid":
			HelloworldAddrGoBuildID = value
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

	ProgramGoRoutinesNoDwarf = ProgramGoRoutines + ".nodwarf"
	if err := buildProgramWithoutDWARF(ProgramGoRoutines+".go", ProgramGoRoutinesNoDwarf); err != nil {
		return err
	}

	updateAddressIfMatched := func(name string, value uint64) error {
		switch name {
		case "main.main":
			GoRoutinesAddrMain = value
		case "main.inc":
			GoRoutinesAddrInc = value
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

	ProgramPanicNoDwarf = ProgramPanic + ".nodwarf"
	if err := buildProgramWithoutDWARF(ProgramPanic+".go", ProgramPanicNoDwarf); err != nil {
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

func buildProgramTypePrint(srcDirname string) error {
	ProgramTypePrint = srcDirname + "/testdata/typeprint"

	if err := buildProgram(ProgramTypePrint); err != nil {
		return err
	}

	updateAddressIfMatched := func(name string, value uint64) error {
		switch name {
		case "main.printBool":
			TypePrintAddrPrintBool = value
		case "main.printInt8":
			TypePrintAddrPrintInt8 = value
		case "main.printInt16":
			TypePrintAddrPrintInt16 = value
		case "main.printInt32":
			TypePrintAddrPrintInt32 = value
		case "main.printInt64":
			TypePrintAddrPrintInt64 = value
		case "main.printUint8":
			TypePrintAddrPrintUint8 = value
		case "main.printUint16":
			TypePrintAddrPrintUint16 = value
		case "main.printUint32":
			TypePrintAddrPrintUint32 = value
		case "main.printUint64":
			TypePrintAddrPrintUint64 = value
		case "main.printFloat32":
			TypePrintAddrPrintFloat32 = value
		case "main.printFloat64":
			TypePrintAddrPrintFloat64 = value
		case "main.printComplex64":
			TypePrintAddrPrintComplex64 = value
		case "main.printComplex128":
			TypePrintAddrPrintComplex128 = value
		case "main.printString":
			TypePrintAddrPrintString = value
		case "main.printArray":
			TypePrintAddrPrintArray = value
		case "main.printSlice":
			TypePrintAddrPrintSlice = value
		case "main.printNilSlice":
			TypePrintAddrPrintNilSlice = value
		case "main.printStruct":
			TypePrintAddrPrintStruct = value
		case "main.printPtr":
			TypePrintAddrPrintPtr = value
		case "main.printFunc":
			TypePrintAddrPrintFunc = value
		case "main.printInterface":
			TypePrintAddrPrintInterface = value
		case "main.printPtrInterface":
			TypePrintAddrPrintPtrInterface = value
		case "main.printNilInterface":
			TypePrintAddrPrintNilInterface = value
		case "main.printEmptyInterface":
			TypePrintAddrPrintEmptyInterface = value
		case "main.printNilEmptyInterface":
			TypePrintAddrPrintNilEmptyInterface = value
		case "main.printMap":
			TypePrintAddrPrintMap = value
		case "main.printNilMap":
			TypePrintAddrPrintNilMap = value
		case "main.printChan":
			TypePrintAddrPrintChan = value
		}
		return nil
	}

	return walkSymbols(ProgramTypePrint, updateAddressIfMatched)
}

func buildProgramStartStop(srcDirname string) error {
	ProgramStartStop = srcDirname + "/testdata/startStop"

	if err := buildProgram(ProgramStartStop); err != nil {
		return err
	}

	updateAddressIfMatched := func(name string, value uint64) error {
		switch name {
		case "main.tracedFunc":
			StartStopAddrTracedFunc = value
		case "github.com/ks888/tgo/lib/tracer.Off":
			StartStopAddrTracerOff = value
		}
		return nil
	}

	return walkSymbols(ProgramStartStop, updateAddressIfMatched)
}

func buildProgramStartOnly(srcDirname string) error {
	ProgramStartOnly = srcDirname + "/testdata/startOnly"

	return buildProgram(ProgramStartOnly)
}

func buildProgram(programName string) error {
	// Optimization is enabled, because the tool aims to work well even if the binary is optimized.
	linkOptions := ""
	if strings.HasPrefix(runtime.Version(), "go1.11") {
		linkOptions = "-compressdwarf=false" // not required, but useful for debugging.
	}
	src := programName + ".go"
	if out, err := exec.Command(goBinaryPath, "build", "-ldflags", linkOptions, "-o", programName, src).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build %s: %v\n%v", src, err, string(out))
	}
	return nil
}

func buildProgramWithoutDWARF(srcName, programName string) error {
	if out, err := exec.Command(goBinaryPath, "build", "-ldflags", "-w", "-o", programName, srcName).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build %s: %v\n%v", srcName, err, string(out))
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
