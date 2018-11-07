package tracee

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ks888/tgo/testutils"
)

func TestParseValue(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramTypePrint)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()

	for i, testdata := range []struct {
		funcAddr uint64
		expected string
	}{
		// Note: the test order must be same as the order of functions called in typeprint.
		{funcAddr: testutils.TypePrintAddrPrintBool, expected: "true"},
		{funcAddr: testutils.TypePrintAddrPrintInt8, expected: "-1"},
		{funcAddr: testutils.TypePrintAddrPrintInt16, expected: "-2"},
		{funcAddr: testutils.TypePrintAddrPrintInt32, expected: "-3"},
		{funcAddr: testutils.TypePrintAddrPrintInt64, expected: "-4"},
		{funcAddr: testutils.TypePrintAddrPrintUint8, expected: fmt.Sprintf("%d", ^uint8(0))},
		{funcAddr: testutils.TypePrintAddrPrintUint16, expected: fmt.Sprintf("%d", ^uint16(0))},
		{funcAddr: testutils.TypePrintAddrPrintUint32, expected: fmt.Sprintf("%d", ^uint32(0))},
		{funcAddr: testutils.TypePrintAddrPrintUint64, expected: fmt.Sprintf("%d", ^uint64(0))},
		{funcAddr: testutils.TypePrintAddrPrintFloat32, expected: "0.12345679"},
		{funcAddr: testutils.TypePrintAddrPrintFloat64, expected: "0.12345678901234568"},
		{funcAddr: testutils.TypePrintAddrPrintComplex64, expected: "(1+2i)"},
		{funcAddr: testutils.TypePrintAddrPrintComplex128, expected: "(3+4i)"},
		{funcAddr: testutils.TypePrintAddrPrintString, expected: "\"hello\\n\""},
		{funcAddr: testutils.TypePrintAddrPrintArray, expected: "[2]{1, 2}"},
		{funcAddr: testutils.TypePrintAddrPrintSlice, expected: "[]{3, 4}"},
		{funcAddr: testutils.TypePrintAddrPrintPtr, expected: "&1"},
	} {
		if err := proc.SetBreakpoint(testdata.funcAddr); err != nil {
			t.Fatalf("failed to set breakpoint: %v", err)
		}
		tids, _, err := proc.ContinueAndWait()
		if err != nil {
			t.Fatalf("failed to continue and wait: %v", err)
		}

		threadInfo, err := proc.CurrentThreadInfo(tids[0])
		if err != nil {
			t.Fatalf("failed to get CurrentThreadInfo: %v", err)
		}

		f, err := proc.Binary.FindFunction(testdata.funcAddr)
		if err != nil {
			t.Fatalf("failed to FindFunction: %v", err)
		}

		typ := f.Parameters[0].Typ
		buff := make([]byte, typ.Size())
		if err := proc.debugapiClient.ReadMemory(threadInfo.CurrentStackAddr+8, buff); err != nil {
			t.Fatalf("failed to ReadMemory: %v", err)
		}
		val := (valueParser{reader: proc.debugapiClient}).parseValue(typ, buff, 0)
		if val.String() != testdata.expected {
			t.Errorf("[%d] wrong value: %s", i, val)
		}

		proc.SingleStep(tids[0], testdata.funcAddr)
	}
}

func TestParseValue_NotFixedStringCase(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramTypePrint)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}
	defer proc.Detach()
	go1_11 := GoVersion{MajorVersion: 1, MinorVersion: 11, PatchVersion: 0}

	for _, testdata := range []struct {
		funcAddr        uint64
		testFunc        func(t *testing.T, val value)
		testIfLaterThan GoVersion
	}{
		// Note: the test order must be same as the order of functions called in typeprint.
		{funcAddr: testutils.TypePrintAddrPrintStruct, testFunc: func(t *testing.T, val value) {
			fields := val.(structValue).fields
			if fields["a"].(int64Value).val != 1 || fields["b"].(int64Value).val != 2 {
				t.Errorf("wrong value: %s", fields)
			}
			innerFields := fields["T"].(structValue).fields
			if len(innerFields) != 0 {
				t.Errorf("The fields of 'T' should be empty because the depth is 1. actual: %d", len(innerFields))
			}
		}},
		{funcAddr: testutils.TypePrintAddrPrintFunc, testFunc: func(t *testing.T, val value) {
			if !strings.HasPrefix(val.String(), "0x") {
				t.Errorf("wrong prefix: %s", val)
			}
		}},
		{funcAddr: testutils.TypePrintAddrPrintInterface, testFunc: func(t *testing.T, val value) {
			implVal, ok := val.(interfaceValue).implVal.(structValue)
			if !ok || implVal.StructName != "main.S" {
				t.Fatalf("wrong type: %#v", implVal)
			}
			if implVal.fields["a"].(int64Value).val != 5 {
				t.Errorf("wrong value: %s", implVal.fields)
			}
		}, testIfLaterThan: go1_11},
		{funcAddr: testutils.TypePrintAddrPrintNilInterface, testFunc: func(t *testing.T, val value) {
			if val.String() != "nil" {
				t.Errorf("wrong val: %s", val)
			}
		}},
		{funcAddr: testutils.TypePrintAddrPrintEmptyInterface, testFunc: func(t *testing.T, val value) {
			implVal, ok := val.(interfaceValue).implVal.(structValue)
			if !ok || implVal.StructName != "main.S" {
				t.Fatalf("wrong type: %v", implVal)
			}
			if implVal.fields["a"].(int64Value).val != 9 {
				t.Errorf("wrong value: %s", implVal.fields)
			}
		}, testIfLaterThan: go1_11},
		{funcAddr: testutils.TypePrintAddrPrintNilEmptyInterface, testFunc: func(t *testing.T, val value) {
			if val.String() != "nil" {
				t.Errorf("wrong val: %s", val)
			}
		}},
		{funcAddr: testutils.TypePrintAddrPrintMap, testFunc: func(t *testing.T, val value) {
			mapVal := val.(mapValue)
			if len(mapVal.val) != 10 {
				t.Errorf("wrong len: %d", len(mapVal.val))
			}
			for k, v := range mapVal.val {
				if k.(int64Value).val != v.(int64Value).val {
					t.Errorf("wrong kv: %d, %d", k.(int64Value).val, v.(int64Value).val)
				}
			}
		}},
	} {
		if !proc.Binary.goVersion.LaterThan(testdata.testIfLaterThan) {
			continue
		}

		if err := proc.SetBreakpoint(testdata.funcAddr); err != nil {
			t.Fatalf("failed to set breakpoint: %v", err)
		}
		tids, _, err := proc.ContinueAndWait()
		if err != nil {
			t.Fatalf("failed to continue and wait: %v", err)
		}

		threadInfo, err := proc.CurrentThreadInfo(tids[0])
		if err != nil {
			t.Fatalf("failed to get CurrentThreadInfo: %v", err)
		}

		f, err := proc.Binary.FindFunction(testdata.funcAddr)
		if err != nil {
			t.Fatalf("failed to FindFunction: %v", err)
		}

		typ := f.Parameters[0].Typ
		buff := make([]byte, typ.Size())
		if err := proc.debugapiClient.ReadMemory(threadInfo.CurrentStackAddr+8, buff); err != nil {
			t.Fatalf("failed to ReadMemory: %v", err)
		}
		val := proc.valueParser.parseValue(typ, buff, 1)
		testdata.testFunc(t, val)

		proc.SingleStep(tids[0], testdata.funcAddr)
	}
}
