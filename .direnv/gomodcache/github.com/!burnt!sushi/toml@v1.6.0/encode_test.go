package toml

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRoundtrip(t *testing.T) {
	type scan struct {
		Age        int       `toml:"age"`
		Cats       []string  `toml:"cats"`
		Pi         float64   `toml:"pi"`
		Perfection []int     `toml:"perfection"`
		DOB        time.Time `toml:"dob"`
		IP         net.IP    `toml:"ip"`
		LargeFloat float64   `toml:"large_float"`
	}

	doc := `
age = 13
cats = ["one", "two", "three"]
pi = 3.145
perfection = [11, 2, 3, 4]
dob = 2012-01-02T15:16:17Z
ip = "192.168.59.254"
large_float = 5e+22
`[1:]

	want := scan{
		Age:        13,
		Cats:       []string{"one", "two", "three"},
		Pi:         3.145,
		Perfection: []int{11, 2, 3, 4},
		DOB:        time.Date(2012, 01, 02, 15, 16, 17, 0, time.UTC),
		IP:         net.ParseIP("192.168.59.254"),
		LargeFloat: 5e+22,
	}

	var s scan
	_, err := Decode(doc, &s)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("\nhave: %v\nwant: %v", s, want)
	}

	out, err := Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != doc {
		t.Errorf("\nhave:\n%v\nwant:\n%v", string(out), doc)
	}
}

func TestEncodeArrayHashWithNormalHashOrder(t *testing.T) {
	type Alpha struct {
		V int
	}
	type Beta struct {
		V int
	}
	type Conf struct {
		V int
		A Alpha
		B []Beta
	}

	val := Conf{
		V: 1,
		A: Alpha{2},
		B: []Beta{{3}},
	}
	expected := "V = 1\n\n[A]\n  V = 2\n\n[[B]]\n  V = 3\n"
	encodeExpected(t, "array hash with normal hash order", val, expected, nil)
}

func TestEncodeOmitEmptyStruct(t *testing.T) {
	type (
		T     struct{ Int int }
		Tpriv struct {
			Int     int
			private int
		}
		Ttime struct {
			Time time.Time
		}
	)

	tests := []struct {
		in   any
		want string
	}{
		{struct {
			F T `toml:"f,omitempty"`
		}{}, ""},
		{struct {
			F T `toml:"f,omitempty"`
		}{T{1}}, "[f]\n  Int = 1"},

		{struct {
			F Tpriv `toml:"f,omitempty"`
		}{}, ""},
		{struct {
			F Tpriv `toml:"f,omitempty"`
		}{Tpriv{1, 0}}, "[f]\n  Int = 1"},

		// Private field being set also counts as "not empty".
		{struct {
			F Tpriv `toml:"f,omitempty"`
		}{Tpriv{0, 1}}, "[f]\n  Int = 0"},

		// time.Time is common use case, so test that explicitly.
		{struct {
			F Ttime `toml:"t,omitempty"`
		}{}, ""},
		{struct {
			F Ttime `toml:"t,omitempty"`
		}{Ttime{time.Time{}.Add(1)}}, "[t]\n  Time = 0001-01-01T00:00:00.000000001Z"},

		// TODO: also test with MarshalText, MarshalTOML returning non-zero
		// value.
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			buf := new(bytes.Buffer)

			err := NewEncoder(buf).Encode(tt.in)
			if err != nil {
				t.Fatal(err)
			}

			have := strings.TrimSpace(buf.String())
			if have != tt.want {
				t.Errorf("\nhave:\n%s\nwant:\n%s", have, tt.want)
			}
		})
	}
}

func TestEncodeOmitEmpty(t *testing.T) {
	type comparable struct {
		Bool bool `toml:"bool,omitempty"`
	}
	type uncomparable struct {
		Field []string `toml:"field,omitempty"`
	}
	type nestedUncomparable struct {
		Field uncomparable `toml:"uncomparable,omitempty"`
		Bool  bool         `toml:"bool,omitempty"`
	}
	type simple struct {
		Bool                bool               `toml:"bool,omitempty"`
		String              string             `toml:"string,omitempty"`
		Array               [0]byte            `toml:"array,omitempty"`
		Slice               []int              `toml:"slice,omitempty"`
		Map                 map[string]string  `toml:"map,omitempty"`
		Time                time.Time          `toml:"time,omitempty"`
		Comparable1         comparable         `toml:"comparable1,omitempty"`
		Comparable2         comparable         `toml:"comparable2,omitempty"`
		Uncomparable1       uncomparable       `toml:"uncomparable1,omitempty"`
		Uncomparable2       uncomparable       `toml:"uncomparable2,omitempty"`
		NestedUncomparable1 nestedUncomparable `toml:"nesteduncomparable1,omitempty"`
		NestedUncomparable2 nestedUncomparable `toml:"nesteduncomparable2,omitempty"`
	}

	var v simple
	encodeExpected(t, "fields with omitempty are omitted when empty", v, "", nil)
	v = simple{
		Bool:                true,
		String:              " ",
		Slice:               []int{2, 3, 4},
		Map:                 map[string]string{"foo": "bar"},
		Time:                time.Date(1985, 6, 18, 15, 16, 17, 0, time.UTC),
		Comparable2:         comparable{true},
		Uncomparable2:       uncomparable{[]string{"XXX"}},
		NestedUncomparable1: nestedUncomparable{uncomparable{[]string{"XXX"}}, false},
		NestedUncomparable2: nestedUncomparable{uncomparable{}, true},
	}
	expected := `bool = true
string = " "
slice = [2, 3, 4]
time = 1985-06-18T15:16:17Z

[map]
  foo = "bar"

[comparable2]
  bool = true

[uncomparable2]
  field = ["XXX"]

[nesteduncomparable1]
  [nesteduncomparable1.uncomparable]
    field = ["XXX"]

[nesteduncomparable2]
  bool = true
`
	encodeExpected(t, "fields with omitempty are not omitted when non-empty",
		v, expected, nil)
}

func TestEncodeOmitEmptyPointer(t *testing.T) {
	type s struct {
		String *string `toml:"string,omitempty"`
	}

	t.Run("nil pointers", func(t *testing.T) {
		var v struct {
			String *string            `toml:"string,omitempty"`
			Slice  *[]string          `toml:"slice,omitempty"`
			Map    *map[string]string `toml:"map,omitempty"`
			Struct *s                 `toml:"struct,omitempty"`
		}
		encodeExpected(t, "", v, ``, nil)
	})

	t.Run("zero values", func(t *testing.T) {
		str := ""
		sl := []string{}
		m := map[string]string{}

		v := struct {
			String *string            `toml:"string,omitempty"`
			Slice  *[]string          `toml:"slice,omitempty"`
			Map    *map[string]string `toml:"map,omitempty"`
			Struct *s                 `toml:"struct,omitempty"`
		}{&str, &sl, &m, &s{&str}}
		want := `string = ""
slice = []

[map]

[struct]
  string = ""
`
		encodeExpected(t, "", v, want, nil)
	})

	t.Run("with values", func(t *testing.T) {
		str := "XXX"
		sl := []string{"XXX"}
		m := map[string]string{"XXX": "XXX"}

		v := struct {
			String *string            `toml:"string,omitempty"`
			Slice  *[]string          `toml:"slice,omitempty"`
			Map    *map[string]string `toml:"map,omitempty"`
			Struct *s                 `toml:"struct,omitempty"`
		}{&str, &sl, &m, &s{&str}}
		want := `string = "XXX"
slice = ["XXX"]

[map]
  XXX = "XXX"

[struct]
  string = "XXX"
`
		encodeExpected(t, "", v, want, nil)
	})
}

func TestEncodeOmitZero(t *testing.T) {
	type simple struct {
		Number   int     `toml:"number,omitzero"`
		Real     float64 `toml:"real,omitzero"`
		Unsigned uint    `toml:"unsigned,omitzero"`
	}

	value := simple{0, 0.0, uint(0)}
	expected := ""

	encodeExpected(t, "simple with omitzero, all zero", value, expected, nil)

	value.Number = 10
	value.Real = 20
	value.Unsigned = 5
	expected = `number = 10
real = 20.0
unsigned = 5
`
	encodeExpected(t, "simple with omitzero, non-zero", value, expected, nil)
}

func TestEncodeOmitemptyEmptyName(t *testing.T) {
	type simple struct {
		S []int `toml:",omitempty"`
	}
	v := simple{[]int{1, 2, 3}}
	expected := "S = [1, 2, 3]\n"
	encodeExpected(t, "simple with omitempty, no name, non-empty field",
		v, expected, nil)
}

func TestEncodeAnonymousStruct(t *testing.T) {
	type Inner struct{ N int }
	type inner struct{ B int }
	type Embedded struct {
		Inner1 Inner
		Inner2 Inner
	}
	type Outer0 struct {
		Inner
		inner
	}
	type Outer1 struct {
		Inner `toml:"inner"`
		inner `toml:"innerb"`
	}
	type Outer3 struct {
		Embedded
	}

	v0 := Outer0{Inner{3}, inner{4}}
	expected := "N = 3\nB = 4\n"
	encodeExpected(t, "embedded anonymous untagged struct", v0, expected, nil)

	v1 := Outer1{Inner{3}, inner{4}}
	expected = "[inner]\n  N = 3\n\n[innerb]\n  B = 4\n"
	encodeExpected(t, "embedded anonymous tagged struct", v1, expected, nil)

	v3 := Outer3{Embedded: Embedded{Inner{3}, Inner{4}}}
	expected = "[Inner1]\n  N = 3\n\n[Inner2]\n  N = 4\n"
	encodeExpected(t, "embedded anonymous multiple fields", v3, expected, nil)
}

func TestEncodeAnonymousStructPointerField(t *testing.T) {
	type Inner struct{ N int }
	type Outer0 struct{ *Inner }
	type Outer1 struct {
		*Inner `toml:"inner"`
	}

	v0 := Outer0{}
	expected := ""
	encodeExpected(t, "nil anonymous untagged struct pointer field", v0, expected, nil)

	v0 = Outer0{&Inner{3}}
	expected = "N = 3\n"
	encodeExpected(t, "non-nil anonymous untagged struct pointer field", v0, expected, nil)

	v1 := Outer1{}
	expected = ""
	encodeExpected(t, "nil anonymous tagged struct pointer field", v1, expected, nil)

	v1 = Outer1{&Inner{3}}
	expected = "[inner]\n  N = 3\n"
	encodeExpected(t, "non-nil anonymous tagged struct pointer field", v1, expected, nil)
}

func TestEncodeNestedAnonymousStructs(t *testing.T) {
	type A struct{ A string }
	type B struct{ B string }
	type C struct{ C string }
	type BC struct {
		B
		C
	}
	type Outer struct {
		A
		BC
	}

	v := &Outer{
		A: A{
			A: "a",
		},
		BC: BC{
			B: B{
				B: "b",
			},
			C: C{
				C: "c",
			},
		},
	}

	expected := "A = \"a\"\nB = \"b\"\nC = \"c\"\n"
	encodeExpected(t, "nested anonymous untagged structs", v, expected, nil)
}

type InnerForNextTest struct{ N int }

func (InnerForNextTest) F() {}
func (InnerForNextTest) G() {}

func TestEncodeAnonymousNoStructField(t *testing.T) {
	type Inner interface{ F() }
	type inner interface{ G() }
	type IntS []int
	type intS []int
	type Outer0 struct {
		Inner
		inner
		IntS
		intS
	}

	v0 := Outer0{
		Inner: InnerForNextTest{3},
		inner: InnerForNextTest{4},
		IntS:  []int{5, 6},
		intS:  []int{7, 8},
	}
	expected := "IntS = [5, 6]\n\n[Inner]\n  N = 3\n"
	encodeExpected(t, "non struct anonymous field", v0, expected, nil)
}

func TestEncodeIgnoredFields(t *testing.T) {
	type simple struct {
		Number int `toml:"-"`
	}
	value := simple{}
	expected := ""
	encodeExpected(t, "ignored field", value, expected, nil)
}

func TestEncodeNaN(t *testing.T) {
	s1 := struct {
		Nan float64 `toml:"nan"`
		Inf float64 `toml:"inf"`
	}{math.NaN(), math.Inf(1)}
	s2 := struct {
		Nan float32 `toml:"nan"`
		Inf float32 `toml:"inf"`
	}{float32(math.NaN()), float32(math.Inf(-1))}
	encodeExpected(t, "", s1, "nan = nan\ninf = inf\n", nil)
	encodeExpected(t, "", s2, "nan = nan\ninf = -inf\n", nil)
}

func TestEncodePrimitive(t *testing.T) {
	type MyStruct struct {
		Data  Primitive
		DataA int
		DataB string
	}

	decodeAndEncode := func(toml string) string {
		var s MyStruct
		_, err := Decode(toml, &s)
		if err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		err = NewEncoder(&buf).Encode(s)
		if err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	original := `DataA = 1
DataB = "bbb"
Data = ["Foo", "Bar"]
`
	reEncoded := decodeAndEncode(decodeAndEncode(original))

	if reEncoded != original {
		t.Errorf(
			"re-encoded not the same as original\noriginal:   %q\nre-encoded: %q",
			original, reEncoded)
	}
}

func TestEncodeError(t *testing.T) {
	tests := []struct {
		in      any
		wantErr string
	}{
		{make(chan int), "unsupported type for key '': chan"},
		{struct{ C complex128 }{0}, "unsupported type: complex128"},
		{[]complex128{0}, "unsupported type: complex128"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			err := NewEncoder(os.Stderr).Encode(tt.in)
			if err == nil {
				t.Fatal("err is nil")
			}
			if !errorContains(err, tt.wantErr) {
				t.Errorf("wrong error\nhave: %q\nwant: %q", err, tt.wantErr)
			}
		})
	}
}

type (
	sound struct{ S string }
	food  struct{ F []string }
	fun   func()
	cplx  complex128
	ints  []int

	sound2 struct{ S string }
	food2  struct{ F []string }
	fun2   func()
	cplx2  complex128
	ints2  []int
)

// This is intentionally wrong (pointer receiver)
func (s *sound) MarshalText() ([]byte, error) { return []byte(s.S), nil }
func (f food) MarshalText() ([]byte, error)   { return []byte(strings.Join(f.F, ", ")), nil }
func (f fun) MarshalText() ([]byte, error)    { return []byte("why would you do this?"), nil }
func (c cplx) MarshalText() ([]byte, error) {
	cplx := complex128(c)
	return []byte(fmt.Sprintf("(%f+%fi)", real(cplx), imag(cplx))), nil
}

func intsValue(is []int) []byte {
	var buf bytes.Buffer
	buf.WriteByte('<')
	for i, v := range is {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(strconv.Itoa(v))
	}
	buf.WriteByte('>')
	return buf.Bytes()
}

func (is *ints) MarshalText() ([]byte, error) {
	if is == nil {
		return []byte("[]"), nil
	}
	return intsValue(*is), nil
}

func (s *sound2) MarshalTOML() ([]byte, error) { return []byte("\"" + s.S + "\""), nil }
func (f food2) MarshalTOML() ([]byte, error) {
	return []byte("[\"" + strings.Join(f.F, "\", \"") + "\"]"), nil
}
func (f fun2) MarshalTOML() ([]byte, error) { return []byte("\"why would you do this?\""), nil }
func (c cplx2) MarshalTOML() ([]byte, error) {
	cplx := complex128(c)
	return []byte(fmt.Sprintf("\"(%f+%fi)\"", real(cplx), imag(cplx))), nil
}
func (is *ints2) MarshalTOML() ([]byte, error) {
	// MarshalTOML must quote by self
	if is == nil {
		return []byte(`"[]"`), nil
	}
	return []byte(fmt.Sprintf(`"%s"`, intsValue(*is))), nil
}

func TestEncodeTextMarshaler(t *testing.T) {
	x := struct {
		Name    string
		Labels  map[string]string
		Sound   sound
		Sound2  *sound
		Food    food
		Food2   *food
		Complex cplx
		Fun     fun
		Ints    ints
		Ints2   *ints2
	}{
		Name:   "Goblok",
		Sound:  sound{"miauw"},
		Sound2: &sound{"miauw"},
		Labels: map[string]string{
			"type":  "cat",
			"color": "black",
		},
		Food:    food{[]string{"chicken", "fish"}},
		Food2:   &food{[]string{"chicken", "fish"}},
		Complex: complex(42, 666),
		Fun:     func() { panic("x") },
		Ints:    ints{1, 2, 3, 4},
		Ints2:   &ints2{1, 2, 3, 4},
	}

	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(&x); err != nil {
		t.Fatal(err)
	}

	want := `Name = "Goblok"
Sound = "miauw"
Sound2 = "miauw"
Food = "chicken, fish"
Food2 = "chicken, fish"
Complex = "(42.000000+666.000000i)"
Fun = "why would you do this?"
Ints = "<1,2,3,4>"
Ints2 = "<1,2,3,4>"

[Labels]
  color = "black"
  type = "cat"
`

	if buf.String() != want {
		t.Error("\n" + buf.String())
	}
}

func TestEncodeTOMLMarshaler(t *testing.T) {
	x := struct {
		Name    string
		Labels  map[string]string
		Sound   sound2
		Sound2  *sound2
		Food    food2
		Food2   *food2
		Complex cplx2
		Fun     fun2
	}{
		Name:   "Goblok",
		Sound:  sound2{"miauw"},
		Sound2: &sound2{"miauw"},
		Labels: map[string]string{
			"type":  "cat",
			"color": "black",
		},
		Food:    food2{[]string{"chicken", "fish"}},
		Food2:   &food2{[]string{"chicken", "fish"}},
		Complex: complex(42, 666),
		Fun:     func() { panic("x") },
	}

	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(x); err != nil {
		t.Fatal(err)
	}

	want := `Name = "Goblok"
Sound2 = "miauw"
Food = ["chicken", "fish"]
Food2 = ["chicken", "fish"]
Complex = "(42.000000+666.000000i)"
Fun = "why would you do this?"

[Labels]
  color = "black"
  type = "cat"

[Sound]
  S = "miauw"
`

	if buf.String() != want {
		t.Error("\n" + buf.String())
	}
}

type (
	retNil1 string
	retNil2 string
)

func (r retNil1) MarshalText() ([]byte, error) { return nil, nil }
func (r retNil2) MarshalTOML() ([]byte, error) { return nil, nil }

func TestEncodeEmpty(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		var (
			s   struct{ Text retNil1 }
			buf bytes.Buffer
		)
		err := NewEncoder(&buf).Encode(s)
		if err == nil {
			t.Fatalf("no error, but expected an error; output:\n%s", buf.String())
		}
		if buf.String() != "" {
			t.Error("\n" + buf.String())
		}
	})

	t.Run("toml", func(t *testing.T) {
		var (
			s   struct{ Text retNil2 }
			buf bytes.Buffer
		)
		err := NewEncoder(&buf).Encode(s)
		if err == nil {
			t.Fatalf("no error, but expected an error; output:\n%s", buf.String())
		}
		if buf.String() != "" {
			t.Error("\n" + buf.String())
		}
	})
}

// Would previously fail on 32bit architectures; can test with:
//
//	GOARCH=386         go test -c &&  ./toml.test
//	GOARCH=arm GOARM=7 go test -c && qemu-arm ./toml.test
func TestEncode32bit(t *testing.T) {
	type Inner struct {
		A, B, C string
	}
	type Outer struct{ Inner }

	encodeExpected(t, "embedded anonymous untagged struct",
		Outer{Inner{"a", "b", "c"}},
		"A = \"a\"\nB = \"b\"\nC = \"c\"\n",
		nil)
}

// Skip invalid types if it has toml:"-"
//
// https://github.com/BurntSushi/toml/issues/345
func TestEncodeSkipInvalidType(t *testing.T) {
	buf := new(bytes.Buffer)
	err := NewEncoder(buf).Encode(struct {
		Str  string         `toml:"str"`
		Arr  []func()       `toml:"-"`
		Map  map[string]any `toml:"-"`
		Func func()         `toml:"-"`
	}{
		Str:  "a",
		Arr:  []func(){func() {}},
		Map:  map[string]any{"f": func() {}},
		Func: func() {},
	})
	if err != nil {
		t.Fatal(err)
	}

	have := buf.String()
	want := "str = \"a\"\n"
	if have != want {
		t.Errorf("\nwant: %q\nhave: %q\n", want, have)
	}
}

func TestEncodeDuration(t *testing.T) {
	tests := []time.Duration{
		0,
		time.Second,
		time.Minute,
		time.Hour,
		248*time.Hour + 45*time.Minute + 24*time.Second,
		12345678 * time.Nanosecond,
		12345678 * time.Second,
		4*time.Second + 2*time.Nanosecond,
	}

	for _, tt := range tests {
		encodeExpected(t, tt.String(),
			struct{ Dur time.Duration }{Dur: tt},
			fmt.Sprintf("Dur = %q", tt), nil)
	}
}

type jsonT struct {
	Num  json.Number
	NumP *json.Number
	Arr  []json.Number
	ArrP []*json.Number
	Tbl  map[string]json.Number
	TblP map[string]*json.Number
}

var (
	n2, n4, n6 = json.Number("2"), json.Number("4"), json.Number("6")
	f2, f4, f6 = json.Number("2.2"), json.Number("4.4"), json.Number("6.6")
)

func TestEncodeJSONNumber(t *testing.T) {
	tests := []struct {
		in   jsonT
		want string
	}{
		{jsonT{}, "Num = 0"},
		{jsonT{
			Num:  "1",
			NumP: &n2,
			Arr:  []json.Number{"3"},
			ArrP: []*json.Number{&n4},
			Tbl:  map[string]json.Number{"k1": "5"},
			TblP: map[string]*json.Number{"k2": &n6}}, `
				Num = 1
				NumP = 2
				Arr = [3]
				ArrP = [4]

				[Tbl]
				  k1 = 5

				[TblP]
				  k2 = 6
		`},
		{jsonT{
			Num:  "1.1",
			NumP: &f2,
			Arr:  []json.Number{"3.3"},
			ArrP: []*json.Number{&f4},
			Tbl:  map[string]json.Number{"k1": "5.5"},
			TblP: map[string]*json.Number{"k2": &f6}}, `
				Num = 1.1
				NumP = 2.2
				Arr = [3.3]
				ArrP = [4.4]

				[Tbl]
				  k1 = 5.5

				[TblP]
				  k2 = 6.6
		`},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			var buf bytes.Buffer
			err := NewEncoder(&buf).Encode(tt.in)
			if err != nil {
				t.Fatal(err)
			}

			have := strings.TrimSpace(buf.String())
			want := strings.ReplaceAll(strings.TrimSpace(tt.want), "\t", "")
			if have != want {
				t.Errorf("\nwant:\n%s\nhave:\n%s\n", want, have)
			}
		})
	}
}

type (
	StructA struct{ StructB }
	StructB struct{ StructC }
	StructC struct{ StructD }
	StructD struct {
		FieldD1 string
		FieldD2 string
		FieldD3 string
	}
)

func TestEncode(t *testing.T) {
	type (
		Embedded struct {
			Int int `toml:"_int"`
		}
		NonStruct int
		MyInt     int
		IntAndMap struct {
			I int
			M map[string]int
		}
	)

	date := time.Date(2014, 5, 11, 19, 30, 40, 0, time.UTC)
	dateStr := "2014-05-11T19:30:40Z"

	tests := map[string]struct {
		input      any
		wantOutput string
		wantError  error
	}{
		"bool field": {
			input: struct {
				BoolTrue  bool
				BoolFalse bool
			}{true, false},
			wantOutput: "BoolTrue = true\nBoolFalse = false\n",
		},
		"int fields": {
			input: struct {
				Int   int
				Int8  int8
				Int16 int16
				Int32 int32
				Int64 int64
			}{1, 2, 3, 4, 5},
			wantOutput: "Int = 1\nInt8 = 2\nInt16 = 3\nInt32 = 4\nInt64 = 5\n",
		},
		"uint fields": {
			input: struct {
				Uint   uint
				Uint8  uint8
				Uint16 uint16
				Uint32 uint32
				Uint64 uint64
			}{1, 2, 3, 4, 5},
			wantOutput: "Uint = 1\nUint8 = 2\nUint16 = 3\nUint32 = 4" +
				"\nUint64 = 5\n",
		},
		"float fields": {
			input: struct {
				Float32 float32
				Float64 float64
			}{1.5, 2.5},
			wantOutput: "Float32 = 1.5\nFloat64 = 2.5\n",
		},
		"string field": {
			input:      struct{ String string }{"foo"},
			wantOutput: "String = \"foo\"\n",
		},
		"string field with \\n escape": {
			input:      struct{ String string }{"foo\n"},
			wantOutput: "String = \"foo\\n\"\n",
		},
		"string field and unexported field": {
			input: struct {
				String     string
				unexported int
			}{"foo", 0},
			wantOutput: "String = \"foo\"\n",
		},
		"datetime field in UTC": {
			input:      struct{ Date time.Time }{date},
			wantOutput: fmt.Sprintf("Date = %s\n", dateStr),
		},
		"datetime field as primitive": {
			// Using a map here to fail if isStructOrMap() returns true for
			// time.Time.
			input: map[string]any{
				"Date": date,
				"Int":  1,
			},
			wantOutput: fmt.Sprintf("Date = %s\nInt = 1\n", dateStr),
		},
		"array fields": {
			input: struct {
				IntArray0 [0]int
				IntArray3 [3]int
			}{[0]int{}, [3]int{1, 2, 3}},
			wantOutput: "IntArray0 = []\nIntArray3 = [1, 2, 3]\n",
		},
		"slice fields": {
			input: struct{ IntSliceNil, IntSlice0, IntSlice3 []int }{
				nil, []int{}, []int{1, 2, 3},
			},
			wantOutput: "IntSlice0 = []\nIntSlice3 = [1, 2, 3]\n",
		},
		"datetime slices": {
			input: struct{ DatetimeSlice []time.Time }{
				[]time.Time{date, date},
			},
			wantOutput: fmt.Sprintf("DatetimeSlice = [%s, %s]\n",
				dateStr, dateStr),
		},
		"nested arrays and slices": {
			input: struct {
				SliceOfArrays         [][2]int
				ArrayOfSlices         [2][]int
				SliceOfArraysOfSlices [][2][]int
				ArrayOfSlicesOfArrays [2][][2]int
				SliceOfMixedArrays    [][2]any
				ArrayOfMixedSlices    [2][]any
			}{
				[][2]int{{1, 2}, {3, 4}},
				[2][]int{{1, 2}, {3, 4}},
				[][2][]int{
					{
						{1, 2}, {3, 4},
					},
					{
						{5, 6}, {7, 8},
					},
				},
				[2][][2]int{
					{
						{1, 2}, {3, 4},
					},
					{
						{5, 6}, {7, 8},
					},
				},
				[][2]any{
					{1, 2}, {"a", "b"},
				},
				[2][]any{
					{1, 2}, {"a", "b"},
				},
			},
			wantOutput: `SliceOfArrays = [[1, 2], [3, 4]]
ArrayOfSlices = [[1, 2], [3, 4]]
SliceOfArraysOfSlices = [[[1, 2], [3, 4]], [[5, 6], [7, 8]]]
ArrayOfSlicesOfArrays = [[[1, 2], [3, 4]], [[5, 6], [7, 8]]]
SliceOfMixedArrays = [[1, 2], ["a", "b"]]
ArrayOfMixedSlices = [[1, 2], ["a", "b"]]
`,
		},
		"empty slice": {
			input:      struct{ Empty []any }{[]any{}},
			wantOutput: "Empty = []\n",
		},
		"(error) slice with element type mismatch (string and integer)": {
			input:      struct{ Mixed []any }{[]any{1, "a"}},
			wantOutput: "Mixed = [1, \"a\"]\n",
		},
		"(error) slice with element type mismatch (integer and float)": {
			input:      struct{ Mixed []any }{[]any{1, 2.5}},
			wantOutput: "Mixed = [1, 2.5]\n",
		},
		"slice with elems of differing Go types, same TOML types": {
			input: struct {
				MixedInts   []any
				MixedFloats []any
			}{
				[]any{
					int(1), int8(2), int16(3), int32(4), int64(5),
					uint(1), uint8(2), uint16(3), uint32(4), uint64(5),
				},
				[]any{float32(1.5), float64(2.5)},
			},
			wantOutput: "MixedInts = [1, 2, 3, 4, 5, 1, 2, 3, 4, 5]\n" +
				"MixedFloats = [1.5, 2.5]\n",
		},
		"(error) slice w/ element type mismatch (one is nested array)": {
			input: struct{ Mixed []any }{
				[]any{1, []any{2}},
			},
			wantOutput: "Mixed = [1, [2]]\n",
		},
		"(error) slice with 1 nil element": {
			input:     struct{ NilElement1 []any }{[]any{nil}},
			wantError: errArrayNilElement,
		},
		"(error) slice with 1 nil element (and other non-nil elements)": {
			input: struct{ NilElement []any }{
				[]any{1, nil},
			},
			wantError: errArrayNilElement,
		},
		"simple map": {
			input:      map[string]int{"a": 1, "b": 2},
			wantOutput: "a = 1\nb = 2\n",
		},
		"map with any value type": {
			input:      map[string]any{"a": 1, "b": "c"},
			wantOutput: "a = 1\nb = \"c\"\n",
		},
		"map with any value type, some of which are structs": {
			input: map[string]any{
				"a": struct{ Int int }{2},
				"b": 1,
			},
			wantOutput: "b = 1\n\n[a]\n  Int = 2\n",
		},
		"nested map": {
			input: map[string]map[string]int{
				"a": {"b": 1},
				"c": {"d": 2},
			},
			wantOutput: "[a]\n  b = 1\n\n[c]\n  d = 2\n",
		},
		"nested struct": {
			input: struct{ Struct struct{ Int int } }{
				struct{ Int int }{1},
			},
			wantOutput: "[Struct]\n  Int = 1\n",
		},
		"nested struct and non-struct field": {
			input: struct {
				Struct struct{ Int int }
				Bool   bool
			}{struct{ Int int }{1}, true},
			wantOutput: "Bool = true\n\n[Struct]\n  Int = 1\n",
		},
		"2 nested structs": {
			input: struct{ Struct1, Struct2 struct{ Int int } }{
				struct{ Int int }{1}, struct{ Int int }{2},
			},
			wantOutput: "[Struct1]\n  Int = 1\n\n[Struct2]\n  Int = 2\n",
		},
		"deeply nested structs": {
			input: struct {
				Struct1, Struct2 struct{ Struct3 *struct{ Int int } }
			}{
				struct{ Struct3 *struct{ Int int } }{&struct{ Int int }{1}},
				struct{ Struct3 *struct{ Int int } }{nil},
			},
			wantOutput: "[Struct1]\n  [Struct1.Struct3]\n    Int = 1" +
				"\n\n[Struct2]\n",
		},
		"nested struct with nil struct elem": {
			input: struct {
				Struct struct{ Inner *struct{ Int int } }
			}{
				struct{ Inner *struct{ Int int } }{nil},
			},
			wantOutput: "[Struct]\n",
		},
		"nested struct with no fields": {
			input: struct {
				Struct struct{ Inner struct{} }
			}{
				struct{ Inner struct{} }{struct{}{}},
			},
			wantOutput: "[Struct]\n  [Struct.Inner]\n",
		},
		"struct with tags": {
			input: struct {
				Struct struct {
					Int int `toml:"_int"`
				} `toml:"_struct"`
				Bool bool `toml:"_bool"`
			}{
				struct {
					Int int `toml:"_int"`
				}{1}, true,
			},
			wantOutput: "_bool = true\n\n[_struct]\n  _int = 1\n",
		},
		"embedded struct": {
			input:      struct{ Embedded }{Embedded{1}},
			wantOutput: "_int = 1\n",
		},
		"embedded *struct": {
			input:      struct{ *Embedded }{&Embedded{1}},
			wantOutput: "_int = 1\n",
		},
		"nested embedded struct": {
			input: struct {
				Struct struct{ Embedded } `toml:"_struct"`
			}{struct{ Embedded }{Embedded{1}}},
			wantOutput: "[_struct]\n  _int = 1\n",
		},
		"deeply nested embedded struct": { // #430
			input: StructA{
				StructB: StructB{
					StructC: StructC{
						StructD: StructD{
							FieldD1: "V1",
							FieldD2: "V2",
							FieldD3: "V3",
						},
					},
				},
			},
			wantOutput: "FieldD1 = \"V1\"\nFieldD2 = \"V2\"\nFieldD3 = \"V3\"\n",
		},
		"nested embedded *struct": {
			input: struct {
				Struct struct{ *Embedded } `toml:"_struct"`
			}{struct{ *Embedded }{&Embedded{1}}},
			wantOutput: "[_struct]\n  _int = 1\n",
		},
		"embedded non-struct": {
			input:      struct{ NonStruct }{5},
			wantOutput: "NonStruct = 5\n",
		},
		"array of tables": {
			input: struct {
				Structs []*struct{ Int int } `toml:"struct"`
			}{
				[]*struct{ Int int }{{1}, {3}},
			},
			wantOutput: "[[struct]]\n  Int = 1\n\n[[struct]]\n  Int = 3\n",
		},
		"array of tables order": {
			input: map[string]any{
				"map": map[string]any{
					"zero": 5,
					"arr": []map[string]int{
						{
							"friend": 5,
						},
					},
				},
			},
			wantOutput: "[map]\n  zero = 5\n\n  [[map.arr]]\n    friend = 5\n",
		},
		"empty key name": {
			input:      map[string]int{"": 1},
			wantOutput: `"" = 1` + "\n",
		},
		"key with \\n escape": {
			input:      map[string]string{"\n": "\n"},
			wantOutput: `"\n" = "\n"` + "\n",
		},

		"empty map name": {
			input: map[string]any{
				"": map[string]int{"v": 1},
			},
			wantOutput: "[\"\"]\n  v = 1\n",
		},
		"(error) top-level slice": {
			input:     []struct{ Int int }{{1}, {2}, {3}},
			wantError: errNoKey,
		},
		"(error) map no string key": {
			input:     map[int]string{1: ""},
			wantError: errNonString,
		},
		"(error) map no string key indirect": {
			input:     map[MyInt]string{1: ""},
			wantError: errNonString,
		},

		"tbl-in-arr-struct": {
			input: struct {
				Arr [][]struct{ A, B, C int }
			}{[][]struct{ A, B, C int }{{{1, 2, 3}, {4, 5, 6}}}},
			wantOutput: "Arr = [[{A = 1, B = 2, C = 3}, {A = 4, B = 5, C = 6}]]",
		},

		"tbl-in-arr-map": {
			input: map[string]any{
				"arr": []any{[]any{
					map[string]any{
						"a": []any{"hello", "world"},
						"b": []any{1.12, 4.1},
						"c": 1,
						"d": map[string]any{"e": "E"},
						"f": struct{ A, B int }{1, 2},
						"g": []struct{ A, B int }{{3, 4}, {5, 6}},
					},
				}},
			},
			wantOutput: `arr = [[{a = ["hello", "world"], b = [1.12, 4.1], c = 1, d = {e = "E"}, f = {A = 1, B = 2}, g = [{A = 3, B = 4}, {A = 5, B = 6}]}]]`,
		},

		"slice of slice": {
			input: struct {
				Slices [][]struct{ Int int }
			}{
				[][]struct{ Int int }{{{1}}, {{2}}, {{3}}},
			},
			wantOutput: "Slices = [[{Int = 1}], [{Int = 2}], [{Int = 3}]]",
		},

		"table in array": {
			input: struct {
				A []IntAndMap
			}{A: []IntAndMap{
				{I: 1, M: map[string]int{"c": 2}},
			}},
			wantOutput: "[[A]]\n  I = 1\n  [A.M]\n    c = 2",
		},

		"table in double array": {
			input: struct {
				A [][]IntAndMap
			}{A: [][]IntAndMap{{{
				I: 1,
				M: map[string]int{"c": 2},
			}}}},
			wantOutput: `A = [[{I = 1, M = {c = 2}}]]`,
		},
	}
	for label, test := range tests {
		encodeExpected(t, label, test.input, test.wantOutput, test.wantError)
	}
}

func TestEncodeDoubleTags(t *testing.T) {
	// This writes two "a" keys to the TOML doc, which isn't valid. I don't
	// think it's worth spending effort preventing this: best we can do is issue
	// an error, and should be clear what the problem is anyway. Not even worth
	// documenting really.
	//
	// The json package silently skips these fields, which is worse behaviour
	// IMO.
	s := struct {
		A int `toml:"a"`
		B int `toml:"a"`
		C int `toml:"c"`
	}{1, 2, 3}
	buf := new(strings.Builder)
	err := NewEncoder(buf).Encode(s)
	if err != nil {
		t.Fatal(err)
	}

	want := `a = 1
a = 2
c = 3
`
	if want != buf.String() {
		t.Errorf("\nhave: %s\nwant: %s\n", buf.String(), want)
	}
}

type (
	Doc1 struct{ N string }
	Doc2 struct{ N string }
)

func (d Doc1) MarshalTOML() ([]byte, error) { return []byte(`marshal_toml = "` + d.N + `"`), nil }
func (d Doc2) MarshalText() ([]byte, error) { return []byte(`marshal_text = "` + d.N + `"`), nil }

// MarshalTOML and MarshalText on the top level type, rather than a field.
func TestMarshalDoc(t *testing.T) {
	t.Run("toml", func(t *testing.T) {
		var buf bytes.Buffer
		err := NewEncoder(&buf).Encode(Doc1{"asd"})
		if err != nil {
			t.Fatal(err)
		}

		want := `marshal_toml = "asd"`
		if want != buf.String() {
			t.Errorf("\nhave: %s\nwant: %s\n", buf.String(), want)
		}
	})

	t.Run("text", func(t *testing.T) {
		var buf bytes.Buffer
		err := NewEncoder(&buf).Encode(Doc2{"asd"})
		if err != nil {
			t.Fatal(err)
		}

		want := `"marshal_text = \"asd\""`
		if want != buf.String() {
			t.Errorf("\nhave: %s\nwant: %s\n", buf.String(), want)
		}
	})
}

func encodeExpected(t *testing.T, label string, val any, want string, wantErr error) {
	t.Helper()
	t.Run(label, func(t *testing.T) {
		t.Helper()
		var buf bytes.Buffer
		err := NewEncoder(&buf).Encode(val)
		if err != wantErr {
			if wantErr != nil {
				if wantErr == errAnything && err != nil {
					return
				}
				t.Errorf("wrong error:\nwant: %v\nhave: %v", wantErr, err)
			} else {
				t.Errorf("Encode failed: %s", err)
			}
		}
		if err != nil {
			return
		}

		have := strings.TrimSpace(buf.String())
		want = strings.TrimSpace(want)
		if want != have {
			t.Errorf("\nhave:\n%s\nwant:\n%s\n",
				"\t"+strings.ReplaceAll(have, "\n", "\n\t"),
				"\t"+strings.ReplaceAll(want, "\n", "\n\t"))
		}
	})
}

func TestMapCustomKeytype(t *testing.T) {
	type (
		MyString string
		MyMap    map[MyString]any
	)

	m := MyMap{
		"k1":     "a",
		"nested": MyMap{"k2": "b"},
	}
	have := new(bytes.Buffer)
	err := NewEncoder(have).Encode(m)
	if err != nil {
		t.Fatal(err)
	}

	want := `
k1 = "a"

[nested]
  k2 = "b"
`[1:]

	if have.String() != want {
		t.Fatalf("\nhave: %q\nwant: %q", have, want)
	}

	var m2 MyMap
	_, err = Decode(have.String(), &m2)
	if err != nil {
		t.Fatal(err)
	}
	if h, w := fmt.Sprintf("%s", m2), fmt.Sprintf("%s", m); h != w {
		t.Fatalf("\nhave: %s\nwant: %s", h, w)
	}
}
