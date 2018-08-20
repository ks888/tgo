package tracee

import "testing"

const (
	testdataHelloworld         = "testdata/helloworld"
	testdataHelloworldMacho    = testdataHelloworld + ".macho"
	testdataHelloworldStripped = testdataHelloworld + ".stripped"
)

func TestNewBinary(t *testing.T) {
	binary, err := NewBinary(testdataHelloworld)
	if err != nil {
		t.Fatalf("failed to create new binary: %v", err)
	}

	if binary.dwarfData == nil {
		t.Errorf("empty data: %v", binary)
	}
}

func TestNewBinary_ProgramNotFound(t *testing.T) {
	_, err := NewBinary("./notexist")
	if err == nil {
		t.Fatal("error not returned when the path is invalid")
	}
}

func TestNewBinary_NotELFProgram(t *testing.T) {
	_, err := NewBinary(testdataHelloworldMacho)
	if err == nil {
		t.Fatal("error not returned when the binary is macho")
	}
}

func TestNewBinary_StrippedProgram(t *testing.T) {
	_, err := NewBinary(testdataHelloworldStripped)
	if err == nil {
		t.Fatal("error not returned when the binary is stripped")
	}
}
