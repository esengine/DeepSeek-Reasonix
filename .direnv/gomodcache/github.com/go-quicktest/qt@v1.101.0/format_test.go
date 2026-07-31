// Licensed under the MIT license, see LICENSE file for details.

package qt_test

import (
	"bytes"
	"testing"

	"github.com/go-quicktest/qt"
)

var formatTests = []struct {
	about string
	value any
	want  string
}{{
	about: "error value",
	value: errBadWolf,
	want:  "bad wolf\n  file:line",
}, {
	about: "error value: not formatted",
	value: &errTest{
		msg: "exterminate!",
	},
	want: `e"exterminate!"`,
}, {
	about: "error value: with quotes",
	value: &errTest{
		msg: `cannot open "/no/such/file"`,
	},
	want: "e`cannot open \"/no/such/file\"`",
}, {
	about: "error value: multi-line",
	value: &errTest{
		msg: `err:
"these are the voyages"`,
	},
	want: `e"err:\n\"these are the voyages\""`,
}, {
	about: "error value: with backquotes",
	value: &errTest{
		msg: "cannot `open` \"file\"",
	},
	want: `e"cannot ` + "`open`" + ` \"file\""`,
}, {
	about: "error value: not guarding against nil",
	value: (*errTest)(nil),
	want:  `e<nil>`,
}, {
	about: "stringer",
	value: bytes.NewBufferString("I am a stringer"),
	want:  `s"I am a stringer"`,
}, {
	about: "stringer: with quotes",
	value: bytes.NewBufferString(`I say "hello"`),
	want:  "s`I say \"hello\"`",
}, {
	about: "stringer: not guarding against nil",
	value: (*nilStringer)(nil),
	want:  "s<nil>",
}, {
	about: "string",
	value: "these are the voyages",
	want:  `"these are the voyages"`,
}, {
	about: "string: with quotes",
	value: `here is a quote: "`,
	want:  "`here is a quote: \"`",
}, {
	about: "string: multi-line",
	value: `foo
"bar"
`,
	want: `"foo\n\"bar\"\n"`,
}, {
	about: "string: with backquotes",
	value: `"` + "`",
	want:  `"\"` + "`\"",
}, {
	about: "slice",
	value: []int{1, 2, 3},
	want:  "[]int{1, 2, 3}",
}, {
	about: "bytes",
	value: []byte("hello"),
	want:  `[]uint8("hello")`,
}, {
	about: "custom bytes type",
	value: myBytes("hello"),
	want:  `qt_test.myBytes("hello")`,
}, {
	about: "bytes with backquote",
	value: []byte(`a "b" c`),
	want:  "[]uint8(`a \"b\" c`)",
}, {
	about: "bytes with invalid utf-8",
	value: []byte("\xff"),
	want:  "[]uint8{0xff}",
}, {
	about: "nil byte slice",
	value: []byte(nil),
	want:  "[]uint8(nil)",
}, {
	about: "time",
	value: goTime,
	want:  `s"2012-03-28 00:00:00 +0000 UTC"`,
}, {
	about: "struct with byte slice",
	value: struct{ X []byte }{[]byte("x")},
	want:  "struct { X []uint8 }{\n    X:  {0x78},\n}",
}, {
	about: "uint64",
	value: uint64(17),
	want:  "uint64(17)",
}, {
	about: "uint32",
	value: uint32(17898),
	want:  "uint32(17898)",
}, {
	about: "uintptr",
	value: uintptr(13),
	want:  "uintptr(13)",
},
}

func TestFormat(t *testing.T) {
	for _, test := range formatTests {
		t.Run(test.about, func(t *testing.T) {
			got := qt.Format(test.value)
			if got != test.want {
				t.Fatalf("format:\ngot  %q\nwant %q", got, test.want)
			}
		})
	}
}

type myBytes []byte

// nilStringer is a stringer not guarding against nil.
type nilStringer struct {
	msg string
}

func (s *nilStringer) String() string {
	return s.msg
}
