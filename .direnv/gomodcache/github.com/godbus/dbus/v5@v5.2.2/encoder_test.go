package dbus

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

func TestEncodeArrayOfMaps(t *testing.T) {
	tests := []struct {
		name string
		vs   []any
	}{
		{
			"aligned at 8 at start of array",
			[]any{
				"12345",
				[]map[string]Variant{
					{
						"abcdefg": MakeVariant("foo"),
						"cdef":    MakeVariant(uint32(2)),
					},
				},
			},
		},
		{
			"not aligned at 8 for start of array",
			[]any{
				"1234567890",
				[]map[string]Variant{
					{
						"abcdefg": MakeVariant("foo"),
						"cdef":    MakeVariant(uint32(2)),
					},
				},
			},
		},
	}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, tt := range tests {
			buf := new(bytes.Buffer)
			fds := make([]int, 0)
			enc := newEncoder(buf, order, fds)
			if err := enc.Encode(tt.vs...); err != nil {
				t.Fatal(err)
			}

			dec := newDecoder(buf, order, enc.fds)
			v, err := dec.Decode(SignatureOf(tt.vs...))
			if err != nil {
				t.Errorf("%q: decode (%v) failed: %v", tt.name, order, err)
				continue
			}
			if !reflect.DeepEqual(v, tt.vs) {
				t.Errorf("%q: (%v) not equal: got '%v', want '%v'", tt.name, order, v, tt.vs)
				continue
			}
		}
	}
}

func TestEncodeMapStringInterface(t *testing.T) {
	val := map[string]any{"foo": "bar"}
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	order := binary.LittleEndian
	enc := newEncoder(buf, binary.LittleEndian, fds)
	err := enc.Encode(val)
	if err != nil {
		t.Fatal(err)
	}

	dec := newDecoder(buf, order, enc.fds)
	v, err := dec.Decode(SignatureOf(val))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	if err := Store(v, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, val) {
		t.Errorf("not equal: got '%v', want '%v'",
			out, val)
	}
}

type empty any

func TestEncodeMapStringNamedInterface(t *testing.T) {
	val := map[string]empty{"foo": "bar"}
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	order := binary.LittleEndian
	enc := newEncoder(buf, binary.LittleEndian, fds)
	err := enc.Encode(val)
	if err != nil {
		t.Fatal(err)
	}

	dec := newDecoder(buf, order, enc.fds)
	v, err := dec.Decode(SignatureOf(val))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]empty{}
	if err := Store(v, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, val) {
		t.Errorf("not equal: got '%v', want '%v'",
			out, val)
	}
}

type fooer interface {
	Foo()
}

type fooimpl string

func (fooimpl) Foo() {}

func TestEncodeMapStringNonEmptyInterface(t *testing.T) {
	val := map[string]fooer{"foo": fooimpl("bar")}
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	order := binary.LittleEndian
	enc := newEncoder(buf, binary.LittleEndian, fds)
	err := enc.Encode(val)
	if err != nil {
		t.Fatal(err)
	}

	dec := newDecoder(buf, order, enc.fds)
	v, err := dec.Decode(SignatureOf(val))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]fooer{}
	err = Store(v, &out)
	if err == nil {
		t.Fatal("Shouldn't be able to convert to non empty interfaces")
	}
}

func TestEncodeSliceInterface(t *testing.T) {
	val := []any{"foo", "bar"}
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	order := binary.LittleEndian
	enc := newEncoder(buf, binary.LittleEndian, fds)
	err := enc.Encode(val)
	if err != nil {
		t.Fatal(err)
	}

	dec := newDecoder(buf, order, enc.fds)
	v, err := dec.Decode(SignatureOf(val))
	if err != nil {
		t.Fatal(err)
	}
	out := []any{}
	if err := Store(v, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, val) {
		t.Errorf("not equal: got '%v', want '%v'",
			out, val)
	}
}

func BenchmarkEncodeSliceInterface(b *testing.B) {
	val := []any{"foo", "bar"}
	sig := SignatureOf(val)
	buf := &bytes.Buffer{}
	fds := make([]int, 0)

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		buf.Reset()

		enc := newEncoder(buf, binary.LittleEndian, fds)
		err := enc.Encode(val)
		if err != nil {
			b.Fatal(err)
		}

		dec := newDecoder(buf, binary.LittleEndian, enc.fds)
		_, err = dec.Decode(sig)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestEncodeSliceNamedInterface(t *testing.T) {
	val := []empty{"foo", "bar"}
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	order := binary.LittleEndian
	enc := newEncoder(buf, binary.LittleEndian, fds)
	err := enc.Encode(val)
	if err != nil {
		t.Fatal(err)
	}

	dec := newDecoder(buf, order, enc.fds)
	v, err := dec.Decode(SignatureOf(val))
	if err != nil {
		t.Fatal(err)
	}
	out := []empty{}
	if err := Store(v, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, val) {
		t.Errorf("not equal: got '%v', want '%v'",
			out, val)
	}
}

func TestEncodeNestedInterface(t *testing.T) {
	val := map[string]any{
		"foo": []any{
			"1", "2", "3", "5",
			map[string]any{
				"bar": "baz",
			},
		},
		"bar": map[string]any{
			"baz":  "quux",
			"quux": "quuz",
		},
	}
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	order := binary.LittleEndian
	enc := newEncoder(buf, binary.LittleEndian, fds)
	err := enc.Encode(val)
	if err != nil {
		t.Fatal(err)
	}

	dec := newDecoder(buf, order, enc.fds)
	v, err := dec.Decode(SignatureOf(val))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	if err := Store(v, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, val) {
		t.Errorf("not equal: got '%#v', want '%#v'",
			out, val)
	}
}

func TestEncodeInt(t *testing.T) {
	val := 10
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	order := binary.LittleEndian
	enc := newEncoder(buf, binary.LittleEndian, fds)
	err := enc.Encode(val)
	if err != nil {
		t.Fatal(err)
	}

	dec := newDecoder(buf, order, enc.fds)
	v, err := dec.Decode(SignatureOf(val))
	if err != nil {
		t.Fatal(err)
	}
	var out int
	if err := Store(v, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, val) {
		t.Errorf("not equal: got '%v', want '%v'",
			out, val)
	}
}

func TestEncodeIntToNonCovertible(t *testing.T) {
	val := 150
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	order := binary.LittleEndian
	enc := newEncoder(buf, binary.LittleEndian, fds)
	err := enc.Encode(val)
	if err != nil {
		t.Fatal(err)
	}

	dec := newDecoder(buf, order, enc.fds)
	v, err := dec.Decode(SignatureOf(val))
	if err != nil {
		t.Fatal(err)
	}
	var out bool
	err = Store(v, &out)
	if err == nil {
		t.Logf("%t\n", out)
		t.Fatal("Type mismatch should have occurred")
	}
}

func TestEncodeUint(t *testing.T) {
	val := uint(10)
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	order := binary.LittleEndian
	enc := newEncoder(buf, binary.LittleEndian, fds)
	err := enc.Encode(val)
	if err != nil {
		t.Fatal(err)
	}

	dec := newDecoder(buf, order, enc.fds)
	v, err := dec.Decode(SignatureOf(val))
	if err != nil {
		t.Fatal(err)
	}
	var out uint
	if err := Store(v, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, val) {
		t.Errorf("not equal: got '%v', want '%v'",
			out, val)
	}
}

func TestEncodeUintToNonCovertible(t *testing.T) {
	val := uint(10)
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	order := binary.LittleEndian
	enc := newEncoder(buf, binary.LittleEndian, fds)
	err := enc.Encode(val)
	if err != nil {
		t.Fatal(err)
	}

	dec := newDecoder(buf, order, enc.fds)
	v, err := dec.Decode(SignatureOf(val))
	if err != nil {
		t.Fatal(err)
	}
	var out bool
	err = Store(v, &out)
	if err == nil {
		t.Fatal("Type mismatch should have occurred")
	}
}

type boolean bool

func TestEncodeOfAssignableType(t *testing.T) {
	val := boolean(true)
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	order := binary.LittleEndian
	enc := newEncoder(buf, binary.LittleEndian, fds)
	err := enc.Encode(val)
	if err != nil {
		t.Fatal(err)
	}

	dec := newDecoder(buf, order, enc.fds)
	v, err := dec.Decode(SignatureOf(val))
	if err != nil {
		t.Fatal(err)
	}
	var out boolean
	err = Store(v, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, val) {
		t.Errorf("not equal: got '%v', want '%v'",
			out, val)
	}
}

func TestEncodeVariant(t *testing.T) {
	var res map[ObjectPath]map[string]map[string]Variant
	src := map[ObjectPath]map[string]map[string]Variant{
		ObjectPath("/foo/bar"): {
			"foo": {
				"bar": MakeVariant(10),
				"baz": MakeVariant("20"),
			},
		},
	}
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	order := binary.LittleEndian
	enc := newEncoder(buf, binary.LittleEndian, fds)
	err := enc.Encode(src)
	if err != nil {
		t.Fatal(err)
	}

	dec := newDecoder(buf, order, enc.fds)
	v, err := dec.Decode(SignatureOf(src))
	if err != nil {
		t.Fatal(err)
	}
	err = Store(v, &res)
	if err != nil {
		t.Fatal(err)
	}
	_ = res[ObjectPath("/foo/bar")]["foo"]["baz"].Value().(string)
}

func TestEncodeVariantToList(t *testing.T) {
	var res map[string]Variant
	src := map[string]any{
		"foo": []any{"a", "b", "c"},
	}
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	order := binary.LittleEndian
	enc := newEncoder(buf, binary.LittleEndian, fds)
	err := enc.Encode(src)
	if err != nil {
		t.Fatal(err)
	}

	dec := newDecoder(buf, order, enc.fds)
	v, err := dec.Decode(SignatureOf(src))
	if err != nil {
		t.Fatal(err)
	}
	err = Store(v, &res)
	if err != nil {
		t.Fatal(err)
	}
	_ = res["foo"].Value().([]Variant)
}

func TestEncodeVariantToUint64(t *testing.T) {
	var res map[string]Variant
	src := map[string]any{
		"foo": uint64(10),
	}
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	order := binary.LittleEndian
	enc := newEncoder(buf, binary.LittleEndian, fds)
	err := enc.Encode(src)
	if err != nil {
		t.Fatal(err)
	}

	dec := newDecoder(buf, order, enc.fds)
	v, err := dec.Decode(SignatureOf(src))
	if err != nil {
		t.Fatal(err)
	}
	err = Store(v, &res)
	if err != nil {
		t.Fatal(err)
	}
	_ = res["foo"].Value().(uint64)
}
