package tracee

import (
	"bytes"
	"debug/dwarf"
	"io"
	"math"
	"testing"
)

func TestBuildValue_Int8(t *testing.T) {
	typ := &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 1}}}
	val, ok := (valueBuilder{}).buildValue(typ, []byte{0xff}).(int8Value)
	if !ok {
		t.Fatalf("not int8Value type")
	}
	if val.val != ^int8(0) {
		t.Errorf("wrong value: %#v", val.val)
	}
}

func TestBuildValue_Int16(t *testing.T) {
	typ := &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 2}}}
	val, ok := (valueBuilder{}).buildValue(typ, []byte{0xff, 0xff}).(int16Value)
	if !ok {
		t.Fatalf("not int16Value type")
	}
	if val.val != ^int16(0) {
		t.Errorf("wrong value: %#v", val.val)
	}
}

func TestBuildValue_Int32(t *testing.T) {
	typ := &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 4}}}
	val, ok := (valueBuilder{}).buildValue(typ, []byte{0xff, 0xff, 0xff, 0xff}).(int32Value)
	if !ok {
		t.Fatalf("not int8Value type")
	}
	if val.val != ^int32(0) {
		t.Errorf("wrong value: %#v", val.val)
	}
}

func TestBuildValue_Int64(t *testing.T) {
	typ := &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}}
	val, ok := (valueBuilder{}).buildValue(typ, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}).(int64Value)
	if !ok {
		t.Fatalf("not int8Value type")
	}
	if val.val != ^int64(0) {
		t.Errorf("wrong value: %#v", val.val)
	}
}

func TestBuildValue_Uint8(t *testing.T) {
	typ := &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 1}}}
	val, ok := (valueBuilder{}).buildValue(typ, []byte{0xff}).(uint8Value)
	if !ok {
		t.Fatalf("not uint8Value type")
	}
	if val.val != ^uint8(0) {
		t.Errorf("wrong value: %#v", val.val)
	}
}

func TestBuildValue_Uint16(t *testing.T) {
	typ := &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 2}}}
	val, ok := (valueBuilder{}).buildValue(typ, []byte{0xff, 0xff}).(uint16Value)
	if !ok {
		t.Fatalf("not uint16Value type")
	}
	if val.val != ^uint16(0) {
		t.Errorf("wrong value: %#v", val.val)
	}
}

func TestBuildValue_Uint32(t *testing.T) {
	typ := &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 4}}}
	val, ok := (valueBuilder{}).buildValue(typ, []byte{0xff, 0xff, 0xff, 0xff}).(uint32Value)
	if !ok {
		t.Fatalf("not uint8Value type")
	}
	if val.val != ^uint32(0) {
		t.Errorf("wrong value: %#v", val.val)
	}
}

func TestBuildValue_Uint64(t *testing.T) {
	typ := &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}}
	val, ok := (valueBuilder{}).buildValue(typ, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}).(uint64Value)
	if !ok {
		t.Fatalf("not uint8Value type")
	}
	if val.val != ^uint64(0) {
		t.Errorf("wrong value: %#v", val.val)
	}
}

func TestBuildValue_Float32(t *testing.T) {
	typ := &dwarf.FloatType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 4}}}
	val, ok := (valueBuilder{}).buildValue(typ, []byte{0xff, 0xff, 0x7f, 0x7f}).(float32Value)
	if !ok {
		t.Fatalf("not float32Value type")
	}
	if val.val != math.MaxFloat32 {
		t.Errorf("wrong value: %#v", val.val)
	}
}

func TestBuildValue_Float64(t *testing.T) {
	typ := &dwarf.FloatType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}}
	val, ok := (valueBuilder{}).buildValue(typ, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xef, 0x7f}).(float64Value)
	if !ok {
		t.Fatalf("not float64Value type")
	}
	if val.val != math.MaxFloat64 {
		t.Errorf("wrong value: %#v", val.val)
	}
}

func TestBuildValue_Complext64(t *testing.T) {
	typ := &dwarf.ComplexType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}}
	val, ok := (valueBuilder{}).buildValue(typ, []byte{0xff, 0xff, 0x7f, 0x7f, 0xff, 0xff, 0x7f, 0x7f}).(complex64Value)
	if !ok {
		t.Fatalf("not complex64Value type")
	}
	if real(val.val) != math.MaxFloat32 {
		t.Errorf("wrong real value: %#v", val.val)
	}
	if imag(val.val) != math.MaxFloat32 {
		t.Errorf("wrong imag value: %#v", val.val)
	}
}

func TestBuildValue_Complext128(t *testing.T) {
	typ := &dwarf.ComplexType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 16}}}
	val, ok := (valueBuilder{}).buildValue(typ, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xef, 0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xef, 0x7f}).(complex128Value)
	if !ok {
		t.Fatalf("not complex128Value type")
	}
	if real(val.val) != math.MaxFloat64 {
		t.Errorf("wrong real value: %#v", val.val)
	}
	if imag(val.val) != math.MaxFloat64 {
		t.Errorf("wrong imag value: %#v", val.val)
	}
}

func TestBuildValue_Bool(t *testing.T) {
	val, ok := (valueBuilder{}).buildValue(&dwarf.BoolType{}, []byte{1}).(boolValue)
	if !ok {
		t.Fatalf("not boolValue type")
	}
	if !val.val {
		t.Errorf("wrong value")
	}
}

func TestBuildValue_Pointer(t *testing.T) {
	typ := &dwarf.PtrType{
		Type: &dwarf.BoolType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 1}}},
	}
	reader := mockReader{[]byte{1}}
	val, ok := (valueBuilder{reader: reader}).buildValue(typ, []byte{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8}).(ptrValue)
	if !ok {
		t.Fatalf("not ptrValue type")
	}
	if v, ok := val.pointedVal.(boolValue); !ok || v.val != true {
		t.Errorf("wrong value: %#v", val.pointedVal)
	}
}

func TestBuildValue_String(t *testing.T) {
	typ := &dwarf.StructType{StructName: "string"}
	reader := mockReader{[]byte{'A'}}
	val, ok := (valueBuilder{reader: reader}).buildValue(typ, []byte{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}).(stringValue)
	if !ok {
		t.Fatalf("not stringValue type")
	}
	if val.val != "A" {
		t.Errorf("wrong value: %#v", val.val)
	}
}

func TestBuildValue_Slice(t *testing.T) {
	typ := &dwarf.StructType{StructName: "[]int", Field: []*dwarf.StructField{
		&dwarf.StructField{
			Name: "array",
			Type: &dwarf.PtrType{
				CommonType: dwarf.CommonType{ByteSize: 8},
				Type:       &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
			},
		},
		&dwarf.StructField{
			Name:       "len",
			ByteOffset: 8,
			Type:       &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
		},
		&dwarf.StructField{
			Name:       "cap",
			ByteOffset: 16,
			Type:       &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
		},
	}}
	reader := mockReader{[]byte{0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}}
	v, ok := (valueBuilder{reader: reader}).buildValue(typ, []byte{0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x3, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x3, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}).(sliceValue)
	if !ok {
		t.Fatalf("not sliceValue type")
	}
	if len(v.val) != 3 {
		t.Errorf("wrong length: %d", len(v.val))
	}
	if v.val[0].String() != "1" || v.val[1].String() != "1" || v.val[2].String() != "1" {
		t.Errorf("wrong val: %#v", v.val)
	}
}

func TestBuildValue_Struct(t *testing.T) {
	typ := &dwarf.StructType{StructName: "S", Field: []*dwarf.StructField{
		&dwarf.StructField{
			Name: "F1",
			Type: &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
		},
		&dwarf.StructField{
			Name:       "F2",
			ByteOffset: 8,
			Type:       &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
		},
	}}
	v, ok := (valueBuilder{}).buildValue(typ, []byte{0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}).(structValue)
	if !ok {
		t.Fatalf("not structValue type")
	}
	if v.fields["F1"].String() != "1" {
		t.Errorf("wrong value: %s", v.fields["F1"])
	}
	if v.fields["F2"].String() != "2" {
		t.Errorf("wrong value: %s", v.fields["F2"])
	}
}

func TestBuildValue_Interface(t *testing.T) {
	typeTyp := &dwarf.TypedefType{
		CommonType: dwarf.CommonType{ByteSize: 48, Name: "runtime._type"},
		Type:       &dwarf.StructType{StructName: "runtime._type", CommonType: dwarf.CommonType{ByteSize: 48}},
	}
	itabTyp := &dwarf.TypedefType{
		CommonType: dwarf.CommonType{ByteSize: 32, Name: "runtime.itab"},
		Type: &dwarf.StructType{StructName: "runtime.itab", Field: []*dwarf.StructField{
			&dwarf.StructField{
				Name: "_type",
				Type: &dwarf.PtrType{
					CommonType: dwarf.CommonType{ByteSize: 8},
					Type:       typeTyp,
				},
			},
		}, CommonType: dwarf.CommonType{ByteSize: 32}},
	}
	ifaceTyp := &dwarf.StructType{StructName: "runtime.iface", Field: []*dwarf.StructField{
		&dwarf.StructField{
			Name: "tab",
			Type: &dwarf.PtrType{CommonType: dwarf.CommonType{ByteSize: 8}, Type: itabTyp},
		},
		&dwarf.StructField{
			Name:       "data",
			ByteOffset: 8,
			Type:       &dwarf.PtrType{CommonType: dwarf.CommonType{ByteSize: 8}, Type: &dwarf.VoidType{CommonType: dwarf.CommonType{ByteSize: 0}}},
		},
	}, CommonType: dwarf.CommonType{ByteSize: 16}}

	reader := mockReader{[]byte{0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}}
	mapper := mockRuntimeTypeMapper{&dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}}}
	builder := valueBuilder{reader: reader, mapper: mapper}
	v, ok := builder.buildValue(ifaceTyp, []byte{0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}).(interfaceValue)
	if !ok {
		t.Fatalf("not interfaceValue type")
	}
	if _, ok := v.implType.(*dwarf.IntType); !ok {
		t.Errorf("wrong type: %#v", v.implType)
	}
	if v.implVal.String() != "1" {
		t.Errorf("wrong val: %s", v.implVal)
	}
}

func TestBuildValue_Map(t *testing.T) {
	bmapTyp := &dwarf.StructType{StructName: "bucket<int,int>", Field: []*dwarf.StructField{
		&dwarf.StructField{
			Name: "tophash",
			Type: &dwarf.ArrayType{
				CommonType: dwarf.CommonType{ByteSize: 8},
				Count:      8,
				Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 1}}},
			},
		},
		&dwarf.StructField{
			Name: "keys",
			Type: &dwarf.ArrayType{
				CommonType: dwarf.CommonType{ByteSize: 64},
				Count:      8,
				Type:       &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
			},
			ByteOffset: 8,
		},
		&dwarf.StructField{
			Name: "values",
			Type: &dwarf.ArrayType{
				CommonType: dwarf.CommonType{ByteSize: 64},
				Count:      8,
				Type:       &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
			},
			ByteOffset: 72,
		},
		&dwarf.StructField{
			Name:       "overflow",
			Type:       &dwarf.PtrType{CommonType: dwarf.CommonType{ByteSize: 8}},
			ByteOffset: 136,
		},
	}, CommonType: dwarf.CommonType{ByteSize: 144}}
	bmapTyp.Field[3].Type.(*dwarf.PtrType).Type = bmapTyp

	hmapTyp := &dwarf.StructType{StructName: "hash<int,int>", Field: []*dwarf.StructField{
		&dwarf.StructField{
			Name:       "B",
			Type:       &dwarf.UintType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 1}}},
			ByteOffset: 9,
		},
		&dwarf.StructField{
			Name:       "buckets",
			Type:       &dwarf.PtrType{CommonType: dwarf.CommonType{ByteSize: 8}, Type: bmapTyp},
			ByteOffset: 16,
		},
	}, CommonType: dwarf.CommonType{ByteSize: 48}}

	mapTyp := &dwarf.TypedefType{
		CommonType: dwarf.CommonType{ByteSize: 8, Name: "map[int]int"},
		Type:       &dwarf.PtrType{CommonType: dwarf.CommonType{ByteSize: 8}, Type: hmapTyp},
	}

	bucket1Val := []byte{
		0x4, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // tophash
		0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // keys
		0x2, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // values
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // *overflow
	}
	bucket2Val := []byte{
		0x4, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // tophash
		0x3, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // keys
		0x4, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // values
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // *overflow
	}
	bucketsVal := append(bucket1Val, bucket2Val...)
	hmapVal := []byte{
		0x02, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x1, // B
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
		0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // *buckets
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
	}

	reader := mockStreamReader{bytes.NewReader(append(hmapVal, bucketsVal...))}
	v, ok := (valueBuilder{reader: reader}).buildValue(mapTyp, []byte{0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}).(mapValue)
	if !ok {
		t.Fatalf("not map type")
	}
	if len(v.val) != 2 {
		t.Errorf("wrong len: %d", len(v.val))
	}
	actual := make(map[int64]int64)
	for k, v := range v.val {
		actual[k.(int64Value).val] = v.(int64Value).val
	}
	if actual[1] != 2 {
		t.Errorf("wrong val: %d", actual[1])
	}
	if actual[3] != 4 {
		t.Errorf("wrong val: %d", actual[3])
	}
}

func TestBuildValue_Array(t *testing.T) {
	typ := &dwarf.ArrayType{
		Count:      2,
		CommonType: dwarf.CommonType{ByteSize: 8},
		Type:       &dwarf.IntType{BasicType: dwarf.BasicType{CommonType: dwarf.CommonType{ByteSize: 8}}},
	}
	v, ok := (valueBuilder{}).buildValue(typ, []byte{0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0}).(arrayValue)
	if !ok {
		t.Fatalf("not arrayValue type")
	}
	if len(v.val) != 2 {
		t.Errorf("wrong length: %d", len(v.val))
	}
	if v.val[0].String() != "1" || v.val[1].String() != "2" {
		t.Errorf("wrong val: %#v", v.val)
	}
}

// mockReader always returns the fixed byte array.
type mockReader struct {
	val []byte
}

func (r mockReader) ReadMemory(addr uint64, out []byte) error {
	copy(out, r.val)
	return nil
}

// mockStreamReader uses io.Reader to mock ReadMemory() method.
type mockStreamReader struct {
	io.Reader
}

func (r mockStreamReader) ReadMemory(addr uint64, out []byte) error {
	_, err := r.Reader.Read(out)
	return err
}

type mockRuntimeTypeMapper struct {
	typ dwarf.Type
}

func (r mockRuntimeTypeMapper) MapRuntimeType(addr uint64) (dwarf.Type, error) {
	return r.typ, nil
}
