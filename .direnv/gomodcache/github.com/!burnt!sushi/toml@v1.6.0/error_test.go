package toml_test

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	tomltest "github.com/BurntSushi/toml/internal/toml-test"
)

func TestErrorPosition(t *testing.T) {
	// Note: take care to use leading spaces (not tabs).
	tests := []struct {
		test, err string
	}{
		{"array/missing-separator-02.toml", `
toml: error: expected a comma (',') or array terminator (']'), but got '2'

At line 1, column 13:

      1 | wrong = [ 1 2 3 ]
                      ^`},

		{"array/no-close-01.toml", `
toml: error: expected a comma (',') or array terminator (']'), but got end of file

At line 1, column 24:

      1 | no-close-01 = [ 1, 2, 3
                                 ^`},

		{"array/tables-02.toml", `
toml: error: Key 'fruit.variety' has already been defined.

At line 9, column 4-8:

      7 | 
      8 |   # This table conflicts with the previous table
      9 |   [fruit.variety]
             ^^^^^`},
		{"local-date/trailing-t.toml", `
toml: error: invalid datetime: "2006-01-30T"

At line 2, column 5-15:

      1 | # Date cannot end with trailing T
      2 | d = 2006-01-30T
              ^^^^^^^^^^^`},

		{"key/without-value-01.toml", `
toml: error: expected '.' or '=', but got '\n' instead

At line 1, column 4:

      1 | key
             ^`},
		// Position looks wonky, but test has trailing space, so it's correct.
		{"key/without-value-02.toml", `
toml: error: expected value but found '\n' instead

At line 1, column 7:

      1 | key = 
                ^`},
	}
	fsys := tomltest.EmbeddedTests()
	for _, tt := range tests {
		t.Run(tt.test, func(t *testing.T) {
			input, err := fs.ReadFile(fsys, "invalid/"+tt.test)
			if err != nil {
				t.Fatal(err)
			}

			var x any
			_, err = toml.Decode(string(input), &x)
			if err == nil {
				t.Fatal("err is nil")
			}

			var pErr toml.ParseError
			if !errors.As(err, &pErr) {
				t.Errorf("err is not a ParseError: %T %[1]v", err)
			}

			tt.err = tt.err[1:] + "\n" // Remove first newline, and add trailing.
			want := pErr.ErrorWithUsage()

			if !strings.Contains(want, tt.err) {
				t.Fatalf("\nwant:\n%s\nhave:\n%s", tt.err, want)
			}
		})
	}
}

func TestParseError(t *testing.T) {
	tests := []struct {
		in        any
		toml, err string
	}{
		{
			&struct{ Int int8 }{},
			"Int = 200",
			`| toml: error: 200 is out of range for int8
			 |
			 | At line 1, column 7-9:
			 |
			 |       1 | Int = 200
			 |                 ^^^
			 | Error help:
             |
			 |     This number is too large; this may be an error in the TOML, but it can also be a
			 |     bug in the program that uses too small of an integer.
             |
			 |     The maximum and minimum values are:
             |
			 |         size   │ lowest         │ highest
			 |         ───────┼────────────────┼──────────────
			 |         int8   │ -128           │ 127
			 |         int16  │ -32,768        │ 32,767
			 |         int32  │ -2,147,483,648 │ 2,147,483,647
			 |         int64  │ -9.2 × 10¹⁷    │ 9.2 × 10¹⁷
			 |         uint8  │ 0              │ 255
			 |         uint16 │ 0              │ 65,535
			 |         uint32 │ 0              │ 4,294,967,295
			 |         uint64 │ 0              │ 1.8 × 10¹⁸
             |
			 |     int refers to int32 on 32-bit systems and int64 on 64-bit systems.
			`,
		},
		{
			&struct{ Int int }{},
			fmt.Sprintf("Int = %d", uint64(math.MaxInt64+1)),
			`| toml: error: 9223372036854775808 is out of range for int64
			 |
			 | At line 1, column 7-25:
			 |
			 |       1 | Int = 9223372036854775808
			 |                 ^^^^^^^^^^^^^^^^^^^
			 | Error help:
             |
			 |     This number is too large; this may be an error in the TOML, but it can also be a
			 |     bug in the program that uses too small of an integer.
             |
			 |     The maximum and minimum values are:
             |
			 |         size   │ lowest         │ highest
			 |         ───────┼────────────────┼──────────────
			 |         int8   │ -128           │ 127
			 |         int16  │ -32,768        │ 32,767
			 |         int32  │ -2,147,483,648 │ 2,147,483,647
			 |         int64  │ -9.2 × 10¹⁷    │ 9.2 × 10¹⁷
			 |         uint8  │ 0              │ 255
			 |         uint16 │ 0              │ 65,535
			 |         uint32 │ 0              │ 4,294,967,295
			 |         uint64 │ 0              │ 1.8 × 10¹⁸
             |
			 |     int refers to int32 on 32-bit systems and int64 on 64-bit systems.
			`,
		},
		{
			&struct{ Float float32 }{},
			"Float = 1.1e99",
			`
            | toml: error: 1.1e+99 is out of range for float32
            |
            | At line 1, column 9-14:
            |
            |       1 | Float = 1.1e99
            |                   ^^^^^^
            | Error help:
            |
            |     This number is too large; this may be an error in the TOML, but it can also be a
            |     bug in the program that uses too small of an integer.
            |
            |     The maximum and minimum values are:
            |
            |         size   │ lowest         │ highest
            |         ───────┼────────────────┼──────────────
            |         int8   │ -128           │ 127
            |         int16  │ -32,768        │ 32,767
            |         int32  │ -2,147,483,648 │ 2,147,483,647
            |         int64  │ -9.2 × 10¹⁷    │ 9.2 × 10¹⁷
            |         uint8  │ 0              │ 255
            |         uint16 │ 0              │ 65,535
            |         uint32 │ 0              │ 4,294,967,295
            |         uint64 │ 0              │ 1.8 × 10¹⁸
            |
            |     int refers to int32 on 32-bit systems and int64 on 64-bit systems.
			`,
		},

		{
			&struct{ D time.Duration }{},
			`D = "99 bottles"`,
			`
			| toml: error: invalid duration: "99 bottles"
			|
			| At line 1, column 6-15:
			|
			|       1 | D = "99 bottles"
			|                ^^^^^^^^^^
			| Error help:
			|
			|     A duration must be as "number<unit>", without any spaces. Valid units are:
			|
			|         ns         nanoseconds (billionth of a second)
			|         us, µs     microseconds (millionth of a second)
			|         ms         milliseconds (thousands of a second)
			|         s          seconds
			|         m          minutes
			|         h          hours
			|
			|     You can combine multiple units; for example "5m10s" for 5 minutes and 10
			|     seconds.
			`,
		},

		{
			&struct{ D time.Time }{},
			`D = 2006-01-99`,
			`
            | toml: error: invalid datetime: "2006-01-99"
            |
            | At line 1, column 5-14:
            |
            |       1 | D = 2006-01-99
            |               ^^^^^^^^^^
            | Error help:
            |
            |     A TOML datetime must be in one of the following formats:
            |
            |         2006-01-02T15:04:05Z07:00   Date and time, with timezone.
            |         2006-01-02T15:04:05         Date and time, but without timezone.
            |         2006-01-02                  Date without a time or timezone.
            |         15:04:05                    Just a time, without any timezone.
            |
            |     Seconds may optionally have a fraction, up to nanosecond precision:
            |
            |         15:04:05.123
            |         15:04:05.856018510
			`,
		},

		{
			&struct{ String string }{},
			`string = "test`,
			`
            | toml: error: unexpected EOF; expected '"'
			|
			| At line 1, column 14:
			|
			|       1 | string = "test
			|                        ^
			`,
		},
	}

	prep := func(s string) string {
		lines := strings.Split(strings.TrimSpace(s), "\n")
		for i := range lines {
			if j := strings.IndexByte(lines[i], '|'); j >= 0 {
				lines[i] = lines[i][j+1:]
				lines[i] = strings.Replace(lines[i], " ", "", 1)
			}
		}
		return strings.Join(lines, "\n") + "\n"
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			_, err := toml.Decode(tt.toml, tt.in)
			if err == nil {
				t.Fatalf("err is nil; decoded: %#v", tt.in)
			}

			var pErr toml.ParseError
			if !errors.As(err, &pErr) {
				t.Fatalf("err is not a ParseError: %#v", err)
			}

			tt.err = prep(tt.err)
			have := pErr.ErrorWithUsage()

			// have = strings.ReplaceAll(have, " ", "·")
			// tt.err = strings.ReplaceAll(tt.err, " ", "·")
			if have != tt.err {
				t.Fatalf("\nwant:\n%s\nhave:\n%s", tt.err, have)
			}
		})
	}
}

type Enum1 uint8

func (n *Enum1) UnmarshalText(text []byte) error {
	switch t := strings.TrimSpace(string(text)); t {
	case "ok":
		*n = 1
	default:
		return fmt.Errorf("invalid value: %q", t)
	}
	return nil
}

// Make sure custom types are wrapped in ParseError with correct location.
func TestUnmarshalTypeError(t *testing.T) {
	var c struct {
		K1 string `toml:"k1"`
		K2 Enum1  `toml:"k2"`
		K3 Enum1  `toml:"k3"`
	}
	_, err := toml.Decode("k1 = 'asd'\nk2 = 'ok'\nk3 = 'invalid'\nk4 = 'ok'", &c)
	if err == nil {
		t.Fatal("error is nil")
	}
	var pErr toml.ParseError
	if !errors.As(err, &pErr) {
		t.Fatalf("not a ParseError: %#v", err)
	}

	want := `toml: error: invalid value: "invalid"

At line 3, column 7-13:

      1 | k1 = 'asd'
      2 | k2 = 'ok'
      3 | k3 = 'invalid'
                ^^^^^^^
`

	if have := pErr.ErrorWithUsage(); have != want {
		t.Errorf("\nwant:\n%s\nhave:\n%s", want, have)
	}
}

type Enum2 uint8

func (n *Enum2) UnmarshalTOML(text any) error {
	switch t := strings.TrimSpace(text.(string)); t {
	case "ok":
		*n = 1
	default:
		return fmt.Errorf("invalid value: %q", t)
	}
	return nil
}

func TestMarshalError(t *testing.T) {
	var c struct {
		K1 string `toml:"k1"`
		K2 Enum2  `toml:"k2"`
		K3 Enum2  `toml:"k3"`
	}
	_, err := toml.Decode("k1 = 'asd'\nk2 = 'ok'\nk3 = 'invalid'\nk4 = 'ok'", &c)
	if err == nil {
		t.Fatal("error is nil")
	}
	var pErr toml.ParseError
	if !errors.As(err, &pErr) {
		t.Fatalf("not a ParseError: %#v", err)
	}

	want := `toml: error: invalid value: "invalid"

At line 3, column 7-13:

      1 | k1 = 'asd'
      2 | k2 = 'ok'
      3 | k3 = 'invalid'
                ^^^^^^^
`

	if have := pErr.ErrorWithUsage(); have != want {
		t.Errorf("\nwant:\n%s\nhave:\n%s", want, have)
	}
}

func TestErrorIndent(t *testing.T) {
	getErr := func(t *testing.T, tml string) toml.ParseError {
		var m map[string]any
		_, err := toml.Decode(tml, &m)
		if err == nil {
			t.Fatal(err)
		}
		var pErr toml.ParseError
		if !errors.As(err, &pErr) {
			t.Fatalf("not a ParseError: %#v", err)
		}
		return pErr
	}

	err := getErr(t, "\tspaces = xxx")
	want := `toml: error: expected value but found "xxx" instead

At line 1, column 11-13:

      1 |         spaces = xxx
                           ^^^
`

	if have := err.ErrorWithUsage(); have != want {
		t.Errorf("\nwant:\n%s\nhave:\n%s", want, have)
	}

	err = getErr(t, "\tspaces\t=\txxx")
	want = `toml: error: expected value but found "xxx" instead

At line 1, column 11-13:

      1 |         spaces  =       xxx
                                  ^^^
`
	if have := err.ErrorWithUsage(); have != want {
		t.Errorf("\nwant:\n%s\nhave:\n%s", want, have)
	}

	err = getErr(t, "\txxx \t = \t 1\n\tspaces\t=\txxx")
	want = `toml: error: expected value but found "xxx" instead

At line 2, column 11-13:

      1 |         xxx      =       1
      2 |         spaces  =       xxx
                                  ^^^
`
	if have := err.ErrorWithUsage(); have != want {
		t.Errorf("\nwant:\n%s\nhave:\n%s", want, have)
	}
}
