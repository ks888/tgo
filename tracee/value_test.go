package tracee

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ks888/tgo/testutils"
)

func TestBuildValue(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramTypePrint)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

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
		val := (valueBuilder{reader: proc.debugapiClient}).buildValue(typ, buff)
		if val.String() != testdata.expected {
			t.Errorf("[%d] wrong value: %s", i, val)
		}

		proc.SingleStep(tids[0], testdata.funcAddr)
	}
}

func TestBuildValue_NotFixedStringCase(t *testing.T) {
	proc, err := LaunchProcess(testutils.ProgramTypePrint)
	if err != nil {
		t.Fatalf("failed to launch process: %v", err)
	}

	for _, testdata := range []struct {
		funcAddr uint64
		testFunc func(t *testing.T, val value)
	}{
		// Note: the test order must be same as the order of functions called in typeprint.
		{funcAddr: testutils.TypePrintAddrPrintStruct, testFunc: func(t *testing.T, val value) {
			fields := val.(structValue).fields
			if fields["a"].(int64Value).val != 1 || fields["b"].(int64Value).val != 2 {
				t.Errorf("wrong value: %s", fields)
			}
			innerFields := fields["T"].(structValue).fields
			if innerFields["d"].(int64Value).val != 4 {
				t.Errorf("wrong value: %s", innerFields)
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
				t.Errorf("wrong type: %s", implVal.StructName)
			}
			if implVal.fields["a"].(int64Value).val != 5 {
				t.Errorf("wrong value: %s", implVal.fields)
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
		val := proc.valueBuilder.buildValue(typ, buff)
		testdata.testFunc(t, val)

		proc.SingleStep(tids[0], testdata.funcAddr)
	}
}
