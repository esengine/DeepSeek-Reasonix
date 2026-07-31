package toml

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/BurntSushi/toml/internal"
)

func TestDecodeReader(t *testing.T) {
	var i struct{ A int }
	meta, err := DecodeReader(strings.NewReader("a = 42"), &i)
	if err != nil {
		t.Fatal(err)
	}
	have := fmt.Sprintf("%v %v %v", i, meta.Keys(), meta.Type("a"))
	want := "{42} [a] Integer"
	if have != want {
		t.Errorf("\nhave: %s\nwant: %s", have, want)
	}
}

func TestDecodeFile(t *testing.T) {
	tmp, err := os.CreateTemp("", "toml-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString("a = 42"); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}

	var i struct{ A int }
	meta, err := DecodeFile(tmp.Name(), &i)
	if err != nil {
		t.Fatal(err)
	}

	have := fmt.Sprintf("%v %v %v", i, meta.Keys(), meta.Type("a"))
	want := "{42} [a] Integer"
	if have != want {
		t.Errorf("\nhave: %s\nwant: %s", have, want)
	}
}

func TestDecodeFS(t *testing.T) {
	fsys := fstest.MapFS{
		"test.toml": &fstest.MapFile{
			Data: []byte("a = 42"),
		},
	}

	var i struct{ A int }
	meta, err := DecodeFS(fsys, "test.toml", &i)
	if err != nil {
		t.Fatal(err)
	}
	have := fmt.Sprintf("%v %v %v", i, meta.Keys(), meta.Type("a"))
	want := "{42} [a] Integer"
	if have != want {
		t.Errorf("\nhave: %s\nwant: %s", have, want)
	}
}

func TestDecodeBOM(t *testing.T) {
	for _, tt := range [][]byte{
		[]byte("\xff\xfea = \"b\""),
		[]byte("\xfe\xffa = \"b\""),
		[]byte("\xef\xbb\xbfa = \"b\""),
	} {
		t.Run("", func(t *testing.T) {
			var s struct{ A string }
			_, err := Decode(string(tt), &s)
			if err != nil {
				t.Fatal(err)
			}
			if s.A != "b" {
				t.Errorf(`s.A is not "b" but %q`, s.A)
			}
		})
	}
}

func TestDecodeEmbedded(t *testing.T) {
	type Dog struct{ Name string }
	type Age int
	type cat struct{ Name string }

	for _, test := range []struct {
		label       string
		input       string
		decodeInto  any
		wantDecoded any
	}{
		{
			label:       "embedded struct",
			input:       `Name = "milton"`,
			decodeInto:  &struct{ Dog }{},
			wantDecoded: &struct{ Dog }{Dog{"milton"}},
		},
		{
			label:       "embedded non-nil pointer to struct",
			input:       `Name = "milton"`,
			decodeInto:  &struct{ *Dog }{},
			wantDecoded: &struct{ *Dog }{&Dog{"milton"}},
		},
		{
			label:       "embedded nil pointer to struct",
			input:       ``,
			decodeInto:  &struct{ *Dog }{},
			wantDecoded: &struct{ *Dog }{nil},
		},
		{
			label:       "unexported embedded struct",
			input:       `Name = "socks"`,
			decodeInto:  &struct{ cat }{},
			wantDecoded: &struct{ cat }{cat{"socks"}},
		},
		{
			label:       "embedded int",
			input:       `Age = -5`,
			decodeInto:  &struct{ Age }{},
			wantDecoded: &struct{ Age }{-5},
		},
	} {
		_, err := Decode(test.input, test.decodeInto)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(test.wantDecoded, test.decodeInto) {
			t.Errorf("%s: want decoded == %+v, got %+v",
				test.label, test.wantDecoded, test.decodeInto)
		}
	}
}

func TestDecodeErrors(t *testing.T) {
	tests := []struct {
		s       any
		toml    string
		wantErr string
	}{
		{
			&struct{ V int8 }{},
			`V = 999`,
			`toml: line 1 (last key "V"): 999 is out of range for int8`,
		},
		{
			&struct{ V float32 }{},
			`V = 999999999999999`,
			`toml: line 1 (last key "V"): 999999999999999 is out of the safe float32 range`,
		},
		{
			&struct{ V string }{},
			`V = 5`,
			`toml: line 1 (last key "V"): incompatible types: TOML value has type int64; destination has type string`,
		},
		{
			&struct{ V interface{ ASD() } }{},
			`V = 999`,
			`toml: line 1 (last key "V"): unsupported type interface { ASD() }`,
		},
		{
			&struct{ V struct{ V int } }{},
			`V = 999`,
			`toml: line 1 (last key "V"): type mismatch for struct { V int }: expected table but found int64`,
		},
		{
			&struct{ V [1]int }{},
			`V = [1,2,3]`,
			`toml: line 1 (last key "V"): expected array length 1; got TOML array of length 3`,
		},
		{
			&struct{ V struct{ N int8 } }{},
			`V.N = 999`,
			`toml: line 1 (last key "V.N"): 999 is out of range for int8`,
		},
		{
			&struct{ V struct{ N float32 } }{},
			`V.N = 999999999999999`,
			`toml: line 1 (last key "V.N"): 999999999999999 is out of the safe float32 range`,
		},
		{
			&struct{ F float64 }{},
			`f = 999999e999999`,
			`toml: line 1 (last key "f"): 999999e999999 is out of range for float64`,
		},
		{
			&struct{ V struct{ N string } }{},
			`V.N = 5`,
			`toml: line 1 (last key "V.N"): incompatible types: TOML value has type int64; destination has type string`,
		},
		{
			&struct {
				V struct{ N interface{ ASD() } }
			}{},
			`V.N = 999`,
			`toml: line 1 (last key "V.N"): unsupported type interface { ASD() }`,
		},
		{
			&struct{ V struct{ N struct{ V int } } }{},
			`V.N = 999`,
			`toml: line 1 (last key "V.N"): type mismatch for struct { V int }: expected table but found int64`,
		},
		{
			&struct{ V struct{ N [1]int } }{},
			`V.N = [1,2,3]`,
			`toml: line 1 (last key "V.N"): expected array length 1; got TOML array of length 3`,
		},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			_, err := Decode(tt.toml, tt.s)
			if err == nil {
				t.Fatal("err is nil")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("\nhave: %q\nwant: %q", err, tt.wantErr)
			}
		})
	}
}

func TestDecodeIgnoreFields(t *testing.T) {
	const input = `
Number = 123
- = 234
`
	var s struct {
		Number int `toml:"-"`
	}
	if _, err := Decode(input, &s); err != nil {
		t.Fatal(err)
	}
	if s.Number != 0 {
		t.Errorf("got: %d; want 0", s.Number)
	}
}

func TestDecodePointers(t *testing.T) {
	type Object struct {
		Type        string
		Description string
	}

	type Dict struct {
		NamedObject map[string]*Object
		BaseObject  *Object
		Strptr      *string
		Strptrs     []*string
	}
	s1, s2, s3 := "blah", "abc", "def"
	expected := &Dict{
		Strptr:  &s1,
		Strptrs: []*string{&s2, &s3},
		NamedObject: map[string]*Object{
			"foo": {"FOO", "fooooo!!!"},
			"bar": {"BAR", "ba-ba-ba-ba-barrrr!!!"},
		},
		BaseObject: &Object{"BASE", "da base"},
	}

	ex1 := `
Strptr = "blah"
Strptrs = ["abc", "def"]

[NamedObject.foo]
Type = "FOO"
Description = "fooooo!!!"

[NamedObject.bar]
Type = "BAR"
Description = "ba-ba-ba-ba-barrrr!!!"

[BaseObject]
Type = "BASE"
Description = "da base"
`
	dict := new(Dict)
	_, err := Decode(ex1, dict)
	if err != nil {
		t.Errorf("Decode error: %v", err)
	}
	if !reflect.DeepEqual(expected, dict) {
		t.Fatalf("\n%#v\n!=\n%#v\n", expected, dict)
	}
}

func TestDecodeBadDatetime(t *testing.T) {
	var x struct{ T time.Time }
	for _, s := range []string{"123", "1230"} {
		input := "T = " + s
		if _, err := Decode(input, &x); err == nil {
			t.Errorf("Expected invalid DateTime error for %q", s)
		}
	}
}

type sphere struct {
	Center [3]float64
	Radius float64
}

func TestDecodeArrayWrongSize(t *testing.T) {
	var s1 sphere
	if _, err := Decode(`center = [0.1, 2.3]`, &s1); err == nil {
		t.Fatal("Expected array type mismatch error")
	}
}

func TestDecodeIntOverflow(t *testing.T) {
	type table struct {
		Value int8
	}
	var tab table
	if _, err := Decode(`value = 500`, &tab); err == nil {
		t.Fatal("Expected integer out-of-bounds error.")
	}
}

func TestDecodeFloatOverflow(t *testing.T) {
	tests := []struct {
		value    string
		overflow bool
	}{
		{fmt.Sprintf(`F32 = %f`, math.MaxFloat64), true},
		{fmt.Sprintf(`F32 = %f`, -math.MaxFloat64), true},
		{fmt.Sprintf(`F32 = %f`, math.MaxFloat32*1.1), true},
		{fmt.Sprintf(`F32 = %f`, -math.MaxFloat32*1.1), true},
		{fmt.Sprintf(`F32 = %d`, maxSafeFloat32Int+1), true},
		{fmt.Sprintf(`F32 = %d`, -maxSafeFloat32Int-1), true},
		{fmt.Sprintf(`F64 = %d`, maxSafeFloat64Int+1), true},
		{fmt.Sprintf(`F64 = %d`, -maxSafeFloat64Int-1), true},

		{fmt.Sprintf(`F32 = %f`, math.MaxFloat32), false},
		{fmt.Sprintf(`F32 = %f`, -math.MaxFloat32), false},
		{fmt.Sprintf(`F32 = %d`, maxSafeFloat32Int), false},
		{fmt.Sprintf(`F32 = %d`, -maxSafeFloat32Int), false},
		{fmt.Sprintf(`F64 = %f`, math.MaxFloat64), false},
		{fmt.Sprintf(`F64 = %f`, -math.MaxFloat64), false},
		{fmt.Sprintf(`F64 = %f`, math.MaxFloat32), false},
		{fmt.Sprintf(`F64 = %f`, -math.MaxFloat32), false},
		{fmt.Sprintf(`F64 = %d`, maxSafeFloat64Int), false},
		{fmt.Sprintf(`F64 = %d`, -maxSafeFloat64Int), false},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			var tab struct {
				F32 float32
				F64 float64
			}
			_, err := Decode(tt.value, &tab)

			if tt.overflow && err == nil {
				t.Fatal("expected error, but err is nil")
			}
			if (tt.overflow && !errorContains(err, "out of the safe float") && !errorContains(err, "out of range")) || (!tt.overflow && err != nil) {
				t.Fatalf("unexpected error:\n%T: %[1]v", err)
			}
		})
	}
}

func TestDecodeSignbit(t *testing.T) {
	var m struct {
		N1, N2   float64
		I1, I2   float64
		Z1, Z2   float64
		ZF1, ZF2 float64
	}
	_, err := Decode(`
n1 = nan
n2 = -nan
i1 = inf
i2 = -inf
z1 = 0
z2 = -0
zf1 = 0.0
zf2 = -0.0
`, &m)
	if err != nil {
		t.Fatal(err)
	}

	if h := fmt.Sprintf("%v %v", m.N1, math.Signbit(m.N1)); h != "NaN false" {
		t.Error("N1:", h)
	}
	if h := fmt.Sprintf("%v %v", m.I1, math.Signbit(m.I1)); h != "+Inf false" {
		t.Error("I1:", h)
	}
	if h := fmt.Sprintf("%v %v", m.Z1, math.Signbit(m.Z1)); h != "0 false" {
		t.Error("Z1:", h)
	}
	if h := fmt.Sprintf("%v %v", m.ZF1, math.Signbit(m.ZF1)); h != "0 false" {
		t.Error("ZF1:", h)
	}

	if h := fmt.Sprintf("%v %v", m.N2, math.Signbit(m.N2)); h != "NaN true" {
		t.Error("N2:", h)
	}
	if h := fmt.Sprintf("%v %v", m.I2, math.Signbit(m.I2)); h != "-Inf true" {
		t.Error("I2:", h)
	}
	if h := fmt.Sprintf("%v %v", m.Z2, math.Signbit(m.Z2)); h != "0 false" { // Correct: -0 is same as 0
		t.Error("Z2:", h)
	}
	if h := fmt.Sprintf("%v %v", m.ZF2, math.Signbit(m.ZF2)); h != "-0 true" {
		t.Error("ZF2:", h)
	}

	buf := new(bytes.Buffer)
	err = NewEncoder(buf).Encode(m)
	if err != nil {
		t.Fatal(err)
	}

	want := strings.ReplaceAll(`
		N1 = nan
		N2 = -nan
		I1 = inf
		I2 = -inf
		Z1 = 0.0
		Z2 = 0.0
		ZF1 = 0.0
		ZF2 = -0.0
	`, "\t", "")[1:]
	if buf.String() != want {
		t.Errorf("\nwant:\n%s\nhave:\n%s", want, buf.String())
	}
}

func TestDecodeSizedInts(t *testing.T) {
	type table struct {
		U8  uint8
		U16 uint16
		U32 uint32
		U64 uint64
		U   uint
		I8  int8
		I16 int16
		I32 int32
		I64 int64
		I   int
	}
	answer := table{1, 1, 1, 1, 1, -1, -1, -1, -1, -1}
	toml := `
	u8 = 1
	u16 = 1
	u32 = 1
	u64 = 1
	u = 1
	i8 = -1
	i16 = -1
	i32 = -1
	i64 = -1
	i = -1
	`
	var tab table
	if _, err := Decode(toml, &tab); err != nil {
		t.Fatal(err.Error())
	}
	if answer != tab {
		t.Fatalf("Expected %#v but got %#v", answer, tab)
	}
}

type NopUnmarshalTOML int

func (n *NopUnmarshalTOML) UnmarshalTOML(p any) error {
	*n = 42
	return nil
}

func TestDecodeTypes(t *testing.T) {
	type (
		mystr   string
		myiface any
	)

	for _, tt := range []struct {
		v       any
		want    string
		wantErr string
	}{
		{new(map[string]bool), "&map[F:true]", ""},
		{new(map[mystr]bool), "&map[F:true]", ""},
		{new(NopUnmarshalTOML), "42", ""},
		{new(map[any]bool), "&map[F:true]", ""},
		{new(map[myiface]bool), "&map[F:true]", ""},

		{3, "", `toml: cannot decode to non-pointer "int"`},
		{map[string]any{}, "", `toml: cannot decode to non-pointer "map[string]interface {}"`},

		{(*int)(nil), "", `toml: cannot decode to nil value of "*int"`},
		{(*Unmarshaler)(nil), "", `toml: cannot decode to nil value of "*toml.Unmarshaler"`},
		{nil, "", `toml: cannot decode to non-pointer <nil>`},

		{new(map[int]string), "", "toml: cannot decode to a map with non-string key type"},

		{new(struct{ F int }), "", `toml: line 1 (last key "F"): incompatible types: TOML value has type bool; destination has type integer`},
		{new(map[string]int), "", `toml: line 1 (last key "F"): incompatible types: TOML value has type bool; destination has type integer`},
		{new(int), "", `toml: cannot decode to type int`},
		{new([]int), "", "toml: cannot decode to type []int"},
	} {
		t.Run(fmt.Sprintf("%T", tt.v), func(t *testing.T) {
			_, err := Decode(`F = true`, tt.v)
			if !errorContains(err, tt.wantErr) {
				t.Fatalf("wrong error\nhave: %q\nwant: %q", err, tt.wantErr)
			}

			if err == nil {
				have := fmt.Sprintf("%v", tt.v)
				if n, ok := tt.v.(*NopUnmarshalTOML); ok {
					have = fmt.Sprintf("%v", *n)
				}
				if have != tt.want {
					t.Errorf("\nhave: %s\nwant: %s", have, tt.want)
				}
			}
		})
	}
}

func TestUnmarshaler(t *testing.T) {
	var tomlBlob = `
[dishes.hamboogie]
name = "Hamboogie with fries"
price = 10.99

[[dishes.hamboogie.ingredients]]
name = "Bread Bun"

[[dishes.hamboogie.ingredients]]
name = "Lettuce"

[[dishes.hamboogie.ingredients]]
name = "Real Beef Patty"

[[dishes.hamboogie.ingredients]]
name = "Tomato"

[dishes.eggsalad]
name = "Egg Salad with rice"
price = 3.99

[[dishes.eggsalad.ingredients]]
name = "Egg"

[[dishes.eggsalad.ingredients]]
name = "Mayo"

[[dishes.eggsalad.ingredients]]
name = "Rice"
`
	m := &menu{}
	if _, err := Decode(tomlBlob, m); err != nil {
		t.Fatal(err)
	}

	if len(m.Dishes) != 2 {
		t.Log("two dishes should be loaded with UnmarshalTOML()")
		t.Errorf("expected %d but got %d", 2, len(m.Dishes))
	}

	eggSalad := m.Dishes["eggsalad"]
	if _, ok := any(eggSalad).(dish); !ok {
		t.Errorf("expected a dish")
	}

	if eggSalad.Name != "Egg Salad with rice" {
		t.Errorf("expected the dish to be named 'Egg Salad with rice'")
	}

	if len(eggSalad.Ingredients) != 3 {
		t.Log("dish should be loaded with UnmarshalTOML()")
		t.Errorf("expected %d but got %d", 3, len(eggSalad.Ingredients))
	}

	found := false
	for _, i := range eggSalad.Ingredients {
		if i.Name == "Rice" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Rice was not loaded in UnmarshalTOML()")
	}

	// test on a value - must be passed as *
	o := menu{}
	if _, err := Decode(tomlBlob, &o); err != nil {
		t.Fatal(err)
	}

}

type menu struct {
	Dishes map[string]dish
}

func (m *menu) UnmarshalTOML(p any) error {
	m.Dishes = make(map[string]dish)
	data, _ := p.(map[string]any)
	dishes := data["dishes"].(map[string]any)
	for n, v := range dishes {
		if d, ok := v.(map[string]any); ok {
			nd := dish{}
			nd.UnmarshalTOML(d)
			m.Dishes[n] = nd
		} else {
			return fmt.Errorf("not a dish")
		}
	}
	return nil
}

type dish struct {
	Name        string
	Price       float32
	Ingredients []ingredient
}

func (d *dish) UnmarshalTOML(p any) error {
	data, _ := p.(map[string]any)
	d.Name, _ = data["name"].(string)
	d.Price, _ = data["price"].(float32)
	ingredients, _ := data["ingredients"].([]map[string]any)
	for _, e := range ingredients {
		n, _ := any(e).(map[string]any)
		name, _ := n["name"].(string)
		i := ingredient{name}
		d.Ingredients = append(d.Ingredients, i)
	}
	return nil
}

type ingredient struct {
	Name string
}

func TestDecodePrimitive(t *testing.T) {
	type S struct {
		P Primitive
	}
	type T struct {
		S []int
	}
	slicep := func(s []int) *[]int { return &s }
	arrayp := func(a [2]int) *[2]int { return &a }
	mapp := func(m map[string]int) *map[string]int { return &m }
	for i, tt := range []struct {
		v     any
		input string
		want  any
	}{
		// slices
		{slicep(nil), "", slicep(nil)},
		{slicep([]int{}), "", slicep([]int{})},
		{slicep([]int{1, 2, 3}), "", slicep([]int{1, 2, 3})},
		{slicep(nil), "P = [1,2]", slicep([]int{1, 2})},
		{slicep([]int{}), "P = [1,2]", slicep([]int{1, 2})},
		{slicep([]int{1, 2, 3}), "P = [1,2]", slicep([]int{1, 2})},

		// arrays
		{arrayp([2]int{2, 3}), "", arrayp([2]int{2, 3})},
		{arrayp([2]int{2, 3}), "P = [3,4]", arrayp([2]int{3, 4})},

		// maps
		{mapp(nil), "", mapp(nil)},
		{mapp(map[string]int{}), "", mapp(map[string]int{})},
		{mapp(map[string]int{"a": 1}), "", mapp(map[string]int{"a": 1})},
		{mapp(nil), "[P]\na = 2", mapp(map[string]int{"a": 2})},
		{mapp(map[string]int{}), "[P]\na = 2", mapp(map[string]int{"a": 2})},
		{mapp(map[string]int{"a": 1, "b": 3}), "[P]\na = 2", mapp(map[string]int{"a": 2, "b": 3})},

		// structs
		{&T{nil}, "[P]", &T{nil}},
		{&T{[]int{}}, "[P]", &T{[]int{}}},
		{&T{[]int{1, 2, 3}}, "[P]", &T{[]int{1, 2, 3}}},
		{&T{nil}, "[P]\nS = [1,2]", &T{[]int{1, 2}}},
		{&T{[]int{}}, "[P]\nS = [1,2]", &T{[]int{1, 2}}},
		{&T{[]int{1, 2, 3}}, "[P]\nS = [1,2]", &T{[]int{1, 2}}},
	} {
		var s S
		md, err := Decode(tt.input, &s)
		if err != nil {
			t.Errorf("[%d] Decode error: %s", i, err)
			continue
		}
		if err := md.PrimitiveDecode(s.P, tt.v); err != nil {
			t.Errorf("[%d] PrimitiveDecode error: %s", i, err)
			continue
		}
		if !reflect.DeepEqual(tt.v, tt.want) {
			t.Errorf("[%d] got %#v; want %#v", i, tt.v, tt.want)
		}
	}
}

func TestDecodeDatetime(t *testing.T) {
	// Test here in addition to toml-test to ensure the TZs are correct.
	tz7 := time.FixedZone("", -3600*7)

	for _, tt := range []struct {
		in   string
		want time.Time
	}{
		// Offset datetime
		{"1979-05-27T07:32:00Z", time.Date(1979, 05, 27, 07, 32, 0, 0, time.UTC)},
		{"1979-05-27T07:32:00.999999Z", time.Date(1979, 05, 27, 07, 32, 0, 999999000, time.UTC)},
		{"1979-05-27T00:32:00-07:00", time.Date(1979, 05, 27, 00, 32, 0, 0, tz7)},
		{"1979-05-27T00:32:00.999999-07:00", time.Date(1979, 05, 27, 00, 32, 0, 999999000, tz7)},
		{"1979-05-27T00:32:00.24-07:00", time.Date(1979, 05, 27, 00, 32, 0, 240000000, tz7)},
		{"1979-05-27 07:32:00Z", time.Date(1979, 05, 27, 07, 32, 0, 0, time.UTC)},
		{"1979-05-27t07:32:00z", time.Date(1979, 05, 27, 07, 32, 0, 0, time.UTC)},

		// Make sure the space between the datetime and "#" isn't lexed.
		{"1979-05-27T07:32:12-07:00  # c", time.Date(1979, 05, 27, 07, 32, 12, 0, tz7)},

		// Local times.
		{"1979-05-27T07:32:00", time.Date(1979, 05, 27, 07, 32, 0, 0, internal.LocalDatetime)},
		{"1979-05-27T07:32:00.999999", time.Date(1979, 05, 27, 07, 32, 0, 999999000, internal.LocalDatetime)},
		{"1979-05-27T07:32:00.25", time.Date(1979, 05, 27, 07, 32, 0, 250000000, internal.LocalDatetime)},
		{"1979-05-27", time.Date(1979, 05, 27, 0, 0, 0, 0, internal.LocalDate)},
		{"07:32:00", time.Date(0, 1, 1, 07, 32, 0, 0, internal.LocalTime)},
		{"07:32:00.999999", time.Date(0, 1, 1, 07, 32, 0, 999999000, internal.LocalTime)},
	} {
		t.Run(tt.in, func(t *testing.T) {
			var x struct{ D time.Time }
			input := "d = " + tt.in
			if _, err := Decode(input, &x); err != nil {
				t.Fatalf("got error: %s", err)
			}

			if h, w := x.D.Format(time.RFC3339Nano), tt.want.Format(time.RFC3339Nano); h != w {
				t.Errorf("\nhave: %s\nwant: %s", h, w)
			}
		})
	}
}

func TestDecodeTextUnmarshaler(t *testing.T) {
	tests := []struct {
		name string
		t    any
		toml string
		want string
	}{
		{
			"time.Time",
			struct{ Time time.Time }{},
			"Time = 1987-07-05T05:45:00Z",
			"map[Time:1987-07-05 05:45:00 +0000 UTC]",
		},
		{
			"*time.Time",
			struct{ Time *time.Time }{},
			"Time = 1988-07-05T05:45:00Z",
			"map[Time:1988-07-05 05:45:00 +0000 UTC]",
		},
		{
			"map[string]time.Time",
			struct{ Times map[string]time.Time }{},
			"Times.one = 1989-07-05T05:45:00Z\nTimes.two = 1990-07-05T05:45:00Z",
			"map[Times:map[one:1989-07-05 05:45:00 +0000 UTC two:1990-07-05 05:45:00 +0000 UTC]]",
		},
		{
			"map[string]*time.Time",
			struct{ Times map[string]*time.Time }{},
			"Times.one = 1989-07-05T05:45:00Z\nTimes.two = 1990-07-05T05:45:00Z",
			"map[Times:map[one:1989-07-05 05:45:00 +0000 UTC two:1990-07-05 05:45:00 +0000 UTC]]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(tt.toml, &tt.t)
			if err != nil {
				t.Fatal(err)
			}

			have := fmt.Sprintf("%v", tt.t)
			if have != tt.want {
				t.Errorf("\nhave: %s\nwant: %s", have, tt.want)
			}
		})
	}
}

func TestDecodeDuration(t *testing.T) {
	tests := []struct {
		in                  any
		toml, want, wantErr string
	}{
		{&struct{ T time.Duration }{}, `t = "0s"`,
			"&{0s}", ""},
		{&struct{ T time.Duration }{}, `t = "5m4s"`,
			"&{5m4s}", ""},
		{&struct{ T time.Duration }{}, `t = "4.000000002s"`,
			"&{4.000000002s}", ""},

		{&struct{ T time.Duration }{}, `t = 0`,
			"&{0s}", ""},
		{&struct{ T time.Duration }{}, `t = 12345678`,
			"&{12.345678ms}", ""},

		{&struct{ T *time.Duration }{}, `T = "5s"`,
			"&{5s}", ""},
		{&struct{ T *time.Duration }{}, `T = 5`,
			"&{5ns}", ""},

		{&struct{ T map[string]time.Duration }{}, `T.dur = "5s"`,
			"&{map[dur:5s]}", ""},
		{&struct{ T map[string]*time.Duration }{}, `T.dur = "5s"`,
			"&{map[dur:5s]}", ""},

		{&struct{ T []time.Duration }{}, `T = ["5s"]`,
			"&{[5s]}", ""},
		{&struct{ T []*time.Duration }{}, `T = ["5s"]`,
			"&{[5s]}", ""},

		{&struct{ T time.Duration }{}, `t = "99 bottles of beer"`, "&{0s}", `invalid duration: "99 bottles of beer"`},
		{&struct{ T time.Duration }{}, `t = "one bottle of beer"`, "&{0s}", `invalid duration: "one bottle of beer"`},
		{&struct{ T time.Duration }{}, `t = 1.2`, "&{0s}", "incompatible types:"},
		{&struct{ T time.Duration }{}, `t = {}`, "&{0s}", "incompatible types:"},
		{&struct{ T time.Duration }{}, `t = []`, "&{0s}", "incompatible types:"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			_, err := Decode(tt.toml, tt.in)
			if !errorContains(err, tt.wantErr) {
				t.Fatal(err)
			}

			have := fmt.Sprintf("%s", tt.in)
			if have != tt.want {
				t.Errorf("\nhave: %s\nwant: %s", have, tt.want)
			}
		})
	}
}

func TestDecodeJSONNumber(t *testing.T) {
	tests := []struct {
		in                  any
		toml, want, wantErr string
	}{
		{&struct{ D json.Number }{}, `D = 2`, "&{2}", ""},
		{&struct{ D json.Number }{}, `D = 2.002`, "&{2.002}", ""},
		{&struct{ D *json.Number }{}, `D = 2`, "&{2}", ""},
		{&struct{ D *json.Number }{}, `D = 2.002`, "&{2.002}", ""},
		{&struct{ D []json.Number }{}, `D = [2, 3.03]`, "&{[2 3.03]}", ""},
		{&struct{ D []*json.Number }{}, `D = [2, 3.03]`, "&{[2 3.03]}", ""},
		{&struct{ D map[string]json.Number }{}, `D = {a=2, b=3.03}`, "&{map[a:2 b:3.03]}", ""},
		{&struct{ D map[string]*json.Number }{}, `D = {a=2, b=3.03}`, "&{map[a:2 b:3.03]}", ""},

		{&struct{ D json.Number }{}, `D = {}`, "&{}", "incompatible types"},
		{&struct{ D json.Number }{}, `D = []`, "&{}", "incompatible types"},
		{&struct{ D json.Number }{}, `D = "2"`, "&{}", "incompatible types"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			_, err := Decode(tt.toml, tt.in)
			if !errorContains(err, tt.wantErr) {
				t.Fatal(err)
			}

			have := fmt.Sprintf("%s", tt.in)
			if have != tt.want {
				t.Errorf("\nhave: %s\nwant: %s", have, tt.want)
			}
		})
	}
}

func TestMetaDotConflict(t *testing.T) {
	var m map[string]any
	meta, err := Decode(`
		"a.b" = "str"
		a.b   = 1
		""    = 2
	`, &m)
	if err != nil {
		t.Fatal(err)
	}

	want := `"a.b"=String; a.b=Integer; ""=Integer`
	have := ""
	for i, k := range meta.Keys() {
		if i > 0 {
			have += "; "
		}
		have += k.String() + "=" + meta.Type(k...)
	}
	if have != want {
		t.Errorf("\nhave: %s\nwant: %s", have, want)
	}
}

type (
	Outer struct {
		Int   *InnerInt
		Enum  *Enum
		Slice *InnerArrayString
	}
	Enum             int
	InnerInt         struct{ value int }
	InnerArrayString struct{ value []string }
)

const (
	NoValue Enum = iota
	OtherValue
)

func (e *Enum) Value() string {
	switch *e {
	case OtherValue:
		return "OTHER_VALUE"
	}
	return ""
}

func (e *Enum) MarshalTOML() ([]byte, error) {
	return []byte(`"` + e.Value() + `"`), nil
}

func (e *Enum) UnmarshalTOML(value any) error {
	sValue, ok := value.(string)
	if !ok {
		return fmt.Errorf("value %v is not a string type", value)
	}
	for _, enum := range []Enum{NoValue, OtherValue} {
		if enum.Value() == sValue {
			*e = enum
			return nil
		}
	}
	return errors.New("invalid enum value")
}

func (i *InnerInt) MarshalTOML() ([]byte, error) {
	return []byte(strconv.Itoa(i.value)), nil
}
func (i *InnerInt) UnmarshalTOML(value any) error {
	iValue, ok := value.(int64)
	if !ok {
		return fmt.Errorf("value %v is not a int type", value)
	}
	i.value = int(iValue)
	return nil
}

func (as *InnerArrayString) MarshalTOML() ([]byte, error) {
	return []byte("[\"" + strings.Join(as.value, "\", \"") + "\"]"), nil
}

func (as *InnerArrayString) UnmarshalTOML(value any) error {
	if value != nil {
		asValue, ok := value.([]any)
		if !ok {
			return fmt.Errorf("value %v is not a [] type", value)
		}
		as.value = []string{}
		for _, value := range asValue {
			as.value = append(as.value, value.(string))
		}
	}
	return nil
}

// Test for #341
func TestCustomEncode(t *testing.T) {
	enum := OtherValue
	outer := Outer{
		Int:   &InnerInt{value: 10},
		Enum:  &enum,
		Slice: &InnerArrayString{value: []string{"text1", "text2"}},
	}

	var buf bytes.Buffer
	err := NewEncoder(&buf).Encode(outer)
	if err != nil {
		t.Errorf("Encode failed: %s", err)
	}

	have := strings.TrimSpace(buf.String())
	want := strings.ReplaceAll(strings.TrimSpace(`
		Int = 10
		Enum = "OTHER_VALUE"
		Slice = ["text1", "text2"]
	`), "\t", "")
	if want != have {
		t.Errorf("\nhave: %s\nwant: %s\n", have, want)
	}
}

// Test for #341
func TestCustomDecode(t *testing.T) {
	var outer Outer
	meta, err := Decode(`
		Int = 10
		Enum = "OTHER_VALUE"
		Slice = ["text1", "text2"]
	`, &outer)
	if err != nil {
		t.Fatalf("Decode failed: %s", err)
	}

	if outer.Int.value != 10 {
		t.Errorf("\nhave:\n%v\nwant:\n%v\n", outer.Int.value, 10)
	}
	if *outer.Enum != OtherValue {
		t.Errorf("\nhave:\n%v\nwant:\n%v\n", outer.Enum, OtherValue)
	}
	if fmt.Sprint(outer.Slice.value) != fmt.Sprint([]string{"text1", "text2"}) {
		t.Errorf("\nhave:\n%v\nwant:\n%v\n", outer.Slice.value, []string{"text1", "text2"})
	}
	if len(meta.Undecoded()) > 0 {
		t.Errorf("\ncustom decode leaves unencoded fields: %v\n", meta.Undecoded())
	}
}

// TODO: this should be improved for v2:
// https://github.com/BurntSushi/toml/issues/384
func TestDecodeDoubleTags(t *testing.T) {
	var s struct {
		A int `toml:"a"`
		B int `toml:"a"`
		C int `toml:"c"`
	}
	_, err := Decode(`
		a = 1
		b = 2
		c = 3
	`, &s)
	if err != nil {
		t.Fatal(err)
	}

	want := `{0 0 3}`
	have := fmt.Sprintf("%v", s)
	if want != have {
		t.Errorf("\nhave: %s\nwant: %s\n", have, want)
	}
}

func TestMetaKeys(t *testing.T) {
	tests := []struct {
		in   string
		want []Key
	}{
		{"", []Key{}},
		{"b=1\na=1", []Key{Key{"b"}, Key{"a"}}},
		{"a.b=1\na.a=1", []Key{Key{"a", "b"}, Key{"a", "a"}}}, // TODO: should include "a"
		{"[tbl]\na=1", []Key{Key{"tbl"}, Key{"tbl", "a"}}},
		{"[tbl]\na.a=1", []Key{Key{"tbl"}, Key{"tbl", "a", "a"}}}, // TODO: should include "a.a"
		{"tbl={a=1}", []Key{Key{"tbl"}, Key{"tbl", "a"}}},
		{"tbl={a={b=1}}", []Key{Key{"tbl"}, Key{"tbl", "a"}, Key{"tbl", "a", "b"}}},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			var x any
			meta, err := Decode(tt.in, &x)
			if err != nil {
				t.Fatal(err)
			}

			have := meta.Keys()
			if !reflect.DeepEqual(tt.want, have) {
				t.Errorf("\nhave: %s\nwant: %s\n", have, tt.want)
			}
		})
	}
}

func TestDecodeParallel(t *testing.T) {
	doc, err := os.ReadFile("testdata/Cargo.toml")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := Unmarshal(doc, new(map[string]any))
			if err != nil {
				panic(err)
			}
		}()
	}
	wg.Wait()
}

// errorContains checks if the error message in have contains the text in
// want.
//
// This is safe when have is nil. Use an empty string for want if you want to
// test that err is nil.
func errorContains(have error, want string) bool {
	if have == nil {
		return want == ""
	}
	if want == "" {
		return false
	}
	return strings.Contains(have.Error(), want)
}

func BenchmarkEscapes(b *testing.B) {
	p := new(parser)
	it := item{}
	str := strings.Repeat("hello, world!\n", 10)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		p.replaceEscapes(it, str)
	}
}

func BenchmarkKey(b *testing.B) {
	k := Key{"cargo-credential-macos-keychain", "version"}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = k.String()
	}
}

type UnmarshalTOMLNoop struct {
}

func (cs *UnmarshalTOMLNoop) UnmarshalTOML(data any) error {
	return nil
}

func TestDecodeCustomStructMarkedDecoded(t *testing.T) {
	var s struct {
		Str    UnmarshalTOMLNoop `toml:"str"`
		Int    UnmarshalTOMLNoop `toml:"int"`
		Arr    UnmarshalTOMLNoop `toml:"arr"`
		Tbl    UnmarshalTOMLNoop `toml:"tbl"`
		Aot    UnmarshalTOMLNoop `toml:"aot"`
		Nested UnmarshalTOMLNoop `toml:"nested"`
	}
	meta, err := Decode(`
		str = "bar"
		int   = 1
		arr = [2]

		[tbl]
		b = 3
		inline = {c = 4}

		[[aot]]
		a = 1
		[[aot]]
		a = 1

		[[nested.a]]
		e = 5
		[[nested.a]]
		f = 6
	`, &s)
	if err != nil {
		t.Fatalf("Decode failed: %s", err)
	}

	// Note that even though the custom unmarshaller did not decode all fields
	// as far as the metadata is concerned they are handled. It is the job of
	// the unmarshaller to ensure this or we would need a more powerful
	// interface like UnmarshalTOML(data any, md *MetaData)
	if len(meta.Undecoded()) > 0 {
		t.Errorf("\ncustom decode leaves undecoded fields: %v\n", meta.Undecoded())
	}
}
