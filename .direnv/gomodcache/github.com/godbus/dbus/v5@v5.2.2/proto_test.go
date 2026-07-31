package dbus

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"reflect"
	"testing"
)

var protoTests = []struct {
	vs           []any
	bigEndian    []byte
	littleEndian []byte
}{
	{
		[]any{int32(0)},
		[]byte{0, 0, 0, 0},
		[]byte{0, 0, 0, 0},
	},
	{
		[]any{true, false},
		[]byte{0, 0, 0, 1, 0, 0, 0, 0},
		[]byte{1, 0, 0, 0, 0, 0, 0, 0},
	},
	{
		[]any{byte(0), uint16(12), int16(32), uint32(43)},
		[]byte{0, 0, 0, 12, 0, 32, 0, 0, 0, 0, 0, 43},
		[]byte{0, 0, 12, 0, 32, 0, 0, 0, 43, 0, 0, 0},
	},
	{
		[]any{int64(-1), uint64(1<<64 - 1)},
		bytes.Repeat([]byte{255}, 16),
		bytes.Repeat([]byte{255}, 16),
	},
	{
		[]any{math.Inf(+1)},
		[]byte{0x7f, 0xf0, 0, 0, 0, 0, 0, 0},
		[]byte{0, 0, 0, 0, 0, 0, 0xf0, 0x7f},
	},
	{
		[]any{"foo"},
		[]byte{0, 0, 0, 3, 'f', 'o', 'o', 0},
		[]byte{3, 0, 0, 0, 'f', 'o', 'o', 0},
	},
	{
		[]any{Signature{"ai"}},
		[]byte{2, 'a', 'i', 0},
		[]byte{2, 'a', 'i', 0},
	},
	{
		[]any{[]int16{42, 256}},
		[]byte{0, 0, 0, 4, 0, 42, 1, 0},
		[]byte{4, 0, 0, 0, 42, 0, 0, 1},
	},
	{
		[]any{MakeVariant("foo")},
		[]byte{1, 's', 0, 0, 0, 0, 0, 3, 'f', 'o', 'o', 0},
		[]byte{1, 's', 0, 0, 3, 0, 0, 0, 'f', 'o', 'o', 0},
	},
	{
		[]any{MakeVariant(MakeVariant(Signature{"v"}))},
		[]byte{1, 'v', 0, 1, 'g', 0, 1, 'v', 0},
		[]byte{1, 'v', 0, 1, 'g', 0, 1, 'v', 0},
	},
	{
		[]any{map[int32]bool{42: true}},
		[]byte{0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 42, 0, 0, 0, 1},
		[]byte{8, 0, 0, 0, 0, 0, 0, 0, 42, 0, 0, 0, 1, 0, 0, 0},
	},
	{
		[]any{map[string]Variant{}, byte(42)},
		[]byte{0, 0, 0, 0, 0, 0, 0, 0, 42},
		[]byte{0, 0, 0, 0, 0, 0, 0, 0, 42},
	},
	{
		[]any{[]uint64{}, byte(42)},
		[]byte{0, 0, 0, 0, 0, 0, 0, 0, 42},
		[]byte{0, 0, 0, 0, 0, 0, 0, 0, 42},
	},
	{
		[]any{uint16(1), true},
		[]byte{0, 1, 0, 0, 0, 0, 0, 1},
		[]byte{1, 0, 0, 0, 1, 0, 0, 0},
	},
}

func TestProto(t *testing.T) {
	for i, v := range protoTests {
		buf := new(bytes.Buffer)
		fds := make([]int, 0)
		bigEnc := newEncoder(buf, binary.BigEndian, fds)
		if err := bigEnc.Encode(v.vs...); err != nil {
			t.Fatal(err)
		}
		marshalled := buf.Bytes()
		if !bytes.Equal(marshalled, v.bigEndian) {
			t.Errorf("test %d (marshal be): got '%v', but expected '%v'\n", i+1, marshalled,
				v.bigEndian)
		}
		buf.Reset()
		fds = make([]int, 0)
		litEnc := newEncoder(buf, binary.LittleEndian, fds)
		if err := litEnc.Encode(v.vs...); err != nil {
			t.Fatal(err)
		}
		marshalled = buf.Bytes()
		if !bytes.Equal(marshalled, v.littleEndian) {
			t.Errorf("test %d (marshal le): got '%v', but expected '%v'\n", i+1, marshalled,
				v.littleEndian)
		}
		unmarshalled := reflect.MakeSlice(reflect.TypeOf(v.vs),
			0, 0)
		for i := range v.vs {
			unmarshalled = reflect.Append(unmarshalled,
				reflect.New(reflect.TypeOf(v.vs[i])))
		}
		bigDec := newDecoder(bytes.NewReader(v.bigEndian), binary.BigEndian, make([]int, 0))
		vs, err := bigDec.Decode(SignatureOf(v.vs...))
		if err != nil {
			t.Errorf("test %d (unmarshal be): %s\n", i+1, err)
			continue
		}
		if !reflect.DeepEqual(vs, v.vs) {
			t.Errorf("test %d (unmarshal be): got %#v, but expected %#v\n", i+1, vs, v.vs)
		}
		litDec := newDecoder(bytes.NewReader(v.littleEndian), binary.LittleEndian, make([]int, 0))
		vs, err = litDec.Decode(SignatureOf(v.vs...))
		if err != nil {
			t.Errorf("test %d (unmarshal le): %s\n", i+1, err)
			continue
		}
		if !reflect.DeepEqual(vs, v.vs) {
			t.Errorf("test %d (unmarshal le): got %#v, but expected %#v\n", i+1, vs, v.vs)
		}

	}
}

func TestProtoMap(t *testing.T) {
	m := map[string]uint8{
		"foo": 23,
		"bar": 2,
	}
	var n map[string]uint8
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	enc := newEncoder(buf, binary.LittleEndian, fds)
	if err := enc.Encode(m); err != nil {
		t.Fatal(err)
	}
	dec := newDecoder(buf, binary.LittleEndian, enc.fds)
	vs, err := dec.Decode(Signature{"a{sy}"})
	if err != nil {
		t.Fatal(err)
	}
	if err = Store(vs, &n); err != nil {
		t.Fatal(err)
	}
	if len(n) != 2 || n["foo"] != 23 || n["bar"] != 2 {
		t.Error("got", n)
	}
}

func TestProtoVariantStruct(t *testing.T) {
	var variant Variant
	v := MakeVariant(struct {
		A int32
		B int16
	}{1, 2})
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	enc := newEncoder(buf, binary.LittleEndian, fds)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	dec := newDecoder(buf, binary.LittleEndian, enc.fds)
	vs, err := dec.Decode(Signature{"v"})
	if err != nil {
		t.Fatal(err)
	}
	if err = Store(vs, &variant); err != nil {
		t.Fatal(err)
	}
	sl := variant.Value().([]any)
	v1, v2 := sl[0].(int32), sl[1].(int16)
	if v1 != int32(1) {
		t.Error("got", v1, "as first int")
	}
	if v2 != int16(2) {
		t.Error("got", v2, "as second int")
	}
}

func TestProtoStructTag(t *testing.T) {
	type Bar struct {
		A int32
		B chan any `dbus:"-"`
		C int32
	}
	var bar1, bar2 Bar
	bar1.A = 234
	bar2.C = 345
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	enc := newEncoder(buf, binary.LittleEndian, fds)
	if err := enc.Encode(bar1); err != nil {
		t.Fatal(err)
	}
	dec := newDecoder(buf, binary.LittleEndian, enc.fds)
	vs, err := dec.Decode(Signature{"(ii)"})
	if err != nil {
		t.Fatal(err)
	}
	if err = Store(vs, &bar2); err != nil {
		t.Fatal(err)
	}
	if bar1 != bar2 {
		t.Error("struct tag test: got", bar2)
	}
}

func TestProtoStoreStruct(t *testing.T) {
	var foo struct {
		A int32
		B string
		c chan any
		D any `dbus:"-"`
	}
	src := []any{[]any{int32(42), "foo"}}
	err := Store(src, &foo)
	if err != nil {
		t.Fatal(err)
	}
}

func TestProtoStoreNestedStruct(t *testing.T) {
	var foo struct {
		A int32
		B struct {
			C string
			D float64
		}
	}
	src := []any{
		[]any{
			int32(42),
			[]any{
				"foo",
				3.14,
			},
		},
	}
	err := Store(src, &foo)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMessage(t *testing.T) {
	buf := new(bytes.Buffer)
	message := new(Message)
	message.Type = TypeMethodCall
	message.serial = 32
	message.Headers = map[HeaderField]Variant{
		FieldPath:   MakeVariant(ObjectPath("/org/foo/bar")),
		FieldMember: MakeVariant("baz"),
	}
	message.Body = make([]any, 0)
	err := message.EncodeTo(buf, binary.LittleEndian)
	if err != nil {
		t.Error(err)
	}
	_, err = DecodeMessage(buf)
	if err != nil {
		t.Error(err)
	}
}

func TestProtoStructInterfaces(t *testing.T) {
	b := []byte{42}
	vs, err := newDecoder(bytes.NewReader(b), binary.LittleEndian, make([]int, 0)).Decode(Signature{"(y)"})
	if err != nil {
		t.Fatal(err)
	}
	if vs[0].([]any)[0].(byte) != 42 {
		t.Errorf("wrongs results (got %v)", vs)
	}
}

// ordinary org.freedesktop.DBus.Hello call
var smallMessage = &Message{
	Type:   TypeMethodCall,
	serial: 1,
	Headers: map[HeaderField]Variant{
		FieldDestination: MakeVariant("org.freedesktop.DBus"),
		FieldPath:        MakeVariant(ObjectPath("/org/freedesktop/DBus")),
		FieldInterface:   MakeVariant("org.freedesktop.DBus"),
		FieldMember:      MakeVariant("Hello"),
	},
}

// org.freedesktop.Notifications.Notify
var bigMessage = &Message{
	Type:   TypeMethodCall,
	serial: 2,
	Headers: map[HeaderField]Variant{
		FieldDestination: MakeVariant("org.freedesktop.Notifications"),
		FieldPath:        MakeVariant(ObjectPath("/org/freedesktop/Notifications")),
		FieldInterface:   MakeVariant("org.freedesktop.Notifications"),
		FieldMember:      MakeVariant("Notify"),
		FieldSignature:   MakeVariant(Signature{"susssasa{sv}i"}),
	},
	Body: []any{
		"app_name",
		uint32(0),
		"dialog-information",
		"Notification",
		"This is the body of a notification",
		[]string{"ok", "Ok"},
		map[string]Variant{
			"sound-name": MakeVariant("dialog-information"),
		},
		int32(-1),
	},
}

func BenchmarkDecodeMessageSmall(b *testing.B) {
	var err error
	var rd *bytes.Reader

	b.StopTimer()
	buf := new(bytes.Buffer)
	err = smallMessage.EncodeTo(buf, binary.LittleEndian)
	if err != nil {
		b.Fatal(err)
	}
	decoded := buf.Bytes()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		rd = bytes.NewReader(decoded)
		_, err = DecodeMessage(rd)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeMessageBig(b *testing.B) {
	var err error
	var rd *bytes.Reader

	b.StopTimer()
	buf := new(bytes.Buffer)
	err = bigMessage.EncodeTo(buf, binary.LittleEndian)
	if err != nil {
		b.Fatal(err)
	}
	decoded := buf.Bytes()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		rd = bytes.NewReader(decoded)
		_, err = DecodeMessage(rd)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeMessageSmall(b *testing.B) {
	var err error
	for i := 0; i < b.N; i++ {
		err = smallMessage.EncodeTo(io.Discard, binary.LittleEndian)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeMessageBig(b *testing.B) {
	var err error
	for i := 0; i < b.N; i++ {
		err = bigMessage.EncodeTo(io.Discard, binary.LittleEndian)
		if err != nil {
			b.Fatal(err)
		}
	}
}
