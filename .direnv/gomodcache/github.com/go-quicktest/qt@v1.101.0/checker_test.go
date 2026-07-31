// Licensed under the MIT license, see LICENSE file for details.

package qt_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/go-quicktest/qt"
)

// errTarget is an error implemented as a pointer.
type errTarget struct {
	msg string
}

func (e *errTarget) Error() string {
	return "ptr: " + e.msg
}

// errTargetNonPtr is an error implemented as a non-pointer.
type errTargetNonPtr struct {
	msg string
}

func (e errTargetNonPtr) Error() string {
	return "non ptr: " + e.msg
}

// Fooer is an interface for testing.
type Fooer interface {
	Foo()
}

type cmpType struct {
	Strings []any
	Ints    []int
}

type InnerJSON struct {
	First  string
	Second int             `json:",omitempty" yaml:",omitempty"`
	Third  map[string]bool `json:",omitempty" yaml:",omitempty"`
}

type OuterJSON struct {
	First  float64
	Second []*InnerJSON `json:"Last,omitempty" yaml:"last,omitempty"`
}

type boolean bool

var (
	targetErr = &errTarget{msg: "target"}

	goTime = time.Date(2012, 3, 28, 0, 0, 0, 0, time.UTC)
	chInt  = func() chan int {
		ch := make(chan int, 4)
		ch <- 42
		ch <- 47
		return ch
	}()
	sameInts = cmpopts.SortSlices(func(x, y int) bool {
		return x < y
	})

	cmpEqualsGot = cmpType{
		Strings: []any{"who", "dalek"},
		Ints:    []int{42, 47},
	}
	cmpEqualsWant = cmpType{
		Strings: []any{"who", "dalek"},
		Ints:    []int{42},
	}
)

var checkerTests = []struct {
	about                 string
	checker               qt.Checker
	verbose               bool
	expectedCheckFailure  string
	expectedNegateFailure string
}{{
	about:   "Equals: same values",
	checker: qt.Equals(42, 42),
	expectedNegateFailure: `
error:
  unexpected success
got:
  int(42)
want:
  <same as "got">
`,
}, {
	about:   "Equals: different values",
	checker: qt.Equals("42", "47"),
	expectedCheckFailure: `
error:
  values are not equal
got:
  "42"
want:
  "47"
`,
}, {
	about:   "Equals: different strings with quotes",
	checker: qt.Equals(`string "foo"`, `string "bar"`),
	expectedCheckFailure: tilde2bq(`
error:
  values are not equal
got:
  ~string "foo"~
want:
  ~string "bar"~
`),
}, {
	about:   "Equals: same multiline strings",
	checker: qt.Equals("a\nmultiline\nstring", "a\nmultiline\nstring"),
	expectedNegateFailure: `
error:
  unexpected success
got:
  "a\nmultiline\nstring"
want:
  <same as "got">
`}, {
	about:   "Equals: different multi-line strings",
	checker: qt.Equals("a\nlong\nmultiline\nstring", "just\na\nlong\nmulti-line\nstring\n"),
	expectedCheckFailure: fmt.Sprintf(`
error:
  values are not equal
line diff (-got +want):
%s
got:
  "a\nlong\nmultiline\nstring"
want:
  "just\na\nlong\nmulti-line\nstring\n"
`, diff([]string{"a\n", "long\n", "multiline\n", "string"}, []string{"just\n", "a\n", "long\n", "multi-line\n", "string\n", ""})),
}, {
	about:   "Equals: different single-line strings ending with newline",
	checker: qt.Equals("foo\n", "bar\n"),
	expectedCheckFailure: `
error:
  values are not equal
got:
  "foo\n"
want:
  "bar\n"
`,
}, {
	about:   "Equals: different strings starting with newline",
	checker: qt.Equals("\nfoo", "\nbar"),
	expectedCheckFailure: fmt.Sprintf(`
error:
  values are not equal
line diff (-got +want):
%s
got:
  "\nfoo"
want:
  "\nbar"
`, diff([]string{"\n", "foo"}, []string{"\n", "bar"})),
}, {
	about:   "Equals: different types",
	checker: qt.Equals(42, any("42")),
	expectedCheckFailure: `
error:
  values are not equal
got:
  int(42)
want:
  "42"
`}, {
	about:   "Equals: nil and nil",
	checker: qt.Equals(nil, any(nil)),
	expectedNegateFailure: `
error:
  unexpected success
got:
  nil
want:
  <same as "got">
`,
}, {
	about:   "Equals: error is not nil",
	checker: qt.Equals(error(errBadWolf), error(nil)),
	expectedCheckFailure: `
error:
  got non-nil error
got:
  bad wolf
    file:line
want:
  nil
`}, {
	about: "Equals: error is not nil: not formatted",
	checker: qt.Equals[error](&errTest{
		msg: "bad wolf",
	}, nil),
	expectedCheckFailure: `
error:
  got non-nil error
got:
  e"bad wolf"
want:
  nil
`,
}, {
	about:   "Equals: error does not guard against nil",
	checker: qt.Equals[error]((*errTest)(nil), nil),
	expectedCheckFailure: `
error:
  got non-nil error
got:
  e<nil>
want:
  nil
`,
}, {
	about: "Equals: error is not nil: not formatted and with quotes",
	checker: qt.Equals[error](&errTest{
		msg: `failure: "bad wolf"`,
	}, nil),
	expectedCheckFailure: tilde2bq(`
error:
  got non-nil error
got:
  e~failure: "bad wolf"~
want:
  nil
`),
}, {
	about: "Equals: different errors with same message",
	checker: qt.Equals[error](&errTest{
		msg: "bad wolf",
	}, errors.New("bad wolf")),
	expectedCheckFailure: `
error:
  values are not equal
got type:
  *qt_test.errTest
want type:
  *errors.errorString
got:
  e"bad wolf"
want:
  <same as "got" but different pointer value>
`,
}, {
	about:   "Equals: different pointer errors with the same message",
	checker: qt.Equals(targetErr, &errTarget{msg: "target"}),
	expectedCheckFailure: `
error:
  values are not equal
got:
  e"ptr: target"
want:
  <same as "got" but different pointer value>
`,
}, {
	about:   "Equals: different pointers with the same formatted output",
	checker: qt.Equals(new(int), new(int)),
	expectedCheckFailure: `
error:
  values are not equal
got:
  &int(0)
want:
  <same as "got" but different pointer value>
`,
}, {
	about:   "Equals: nil struct",
	checker: qt.Equals[any]((*struct{})(nil), nil),
	expectedCheckFailure: `
error:
  values are not equal
got:
  (*struct {})(nil)
want:
  nil
`,
}, {
	about:   "Equals: different booleans",
	checker: qt.Equals(true, false),
	expectedCheckFailure: `
error:
  values are not equal
got:
  bool(true)
want:
  bool(false)
`,
}, {
	about: "Equals: uncomparable types",
	checker: qt.Equals[any](struct {
		Ints []int
	}{
		Ints: []int{42, 47},
	}, struct {
		Ints []int
	}{
		Ints: []int{42, 47},
	}),
	expectedCheckFailure: `
error:
  runtime error: comparing uncomparable type struct { Ints []int }
got:
  struct { Ints []int }{
      Ints: {42, 47},
  }
want:
  <same as "got">
`}, {
	about:   "DeepEquals: same values",
	checker: qt.DeepEquals(cmpEqualsGot, cmpEqualsGot),
	expectedNegateFailure: `
error:
  unexpected success
got:
  qt_test.cmpType{
      Strings: {
          "who",
          "dalek",
      },
      Ints: {42, 47},
  }
want:
  <same as "got">
`,
}, {
	about:   "DeepEquals: different values",
	checker: qt.DeepEquals(cmpEqualsGot, cmpEqualsWant),
	expectedCheckFailure: fmt.Sprintf(`
error:
  values are not deep equal
diff (-got +want):
%s
got:
  qt_test.cmpType{
      Strings: {
          "who",
          "dalek",
      },
      Ints: {42, 47},
  }
want:
  qt_test.cmpType{
      Strings: {
          "who",
          "dalek",
      },
      Ints: {42},
  }
`, diff(cmpEqualsGot, cmpEqualsWant)),
}, {
	about:   "DeepEquals: different values: long output",
	checker: qt.DeepEquals([]any{cmpEqualsWant, cmpEqualsWant}, []any{cmpEqualsWant, cmpEqualsWant, 42}),
	expectedCheckFailure: fmt.Sprintf(`
error:
  values are not deep equal
diff (-got +want):
%s
got:
  <suppressed due to length (15 lines), use -v for full output>
want:
  <suppressed due to length (16 lines), use -v for full output>
`, diff([]any{cmpEqualsWant, cmpEqualsWant}, []any{cmpEqualsWant, cmpEqualsWant, 42})),
}, {
	about:   "DeepEquals: different values: long output and verbose",
	checker: qt.DeepEquals([]any{cmpEqualsWant, cmpEqualsWant}, []any{cmpEqualsWant, cmpEqualsWant, 42}),
	verbose: true,
	expectedCheckFailure: fmt.Sprintf(`
error:
  values are not deep equal
diff (-got +want):
%s
got:
  []interface {}{
      qt_test.cmpType{
          Strings: {
              "who",
              "dalek",
          },
          Ints: {42},
      },
      qt_test.cmpType{
          Strings: {
              "who",
              "dalek",
          },
          Ints: {42},
      },
  }
want:
  []interface {}{
      qt_test.cmpType{
          Strings: {
              "who",
              "dalek",
          },
          Ints: {42},
      },
      qt_test.cmpType{
          Strings: {
              "who",
              "dalek",
          },
          Ints: {42},
      },
      int(42),
  }
`, diff([]any{cmpEqualsWant, cmpEqualsWant}, []any{cmpEqualsWant, cmpEqualsWant, 42})),
}, {
	about:   "CmpEquals: different values, long output",
	checker: qt.CmpEquals([]any{cmpEqualsWant, "extra line 1", "extra line 2", "extra line 3"}, []any{cmpEqualsWant, "extra line 1"}),
	expectedCheckFailure: fmt.Sprintf(`
error:
  values are not deep equal
diff (-got +want):
%s
got:
  <suppressed due to length (11 lines), use -v for full output>
want:
  []interface {}{
      qt_test.cmpType{
          Strings: {
              "who",
              "dalek",
          },
          Ints: {42},
      },
      "extra line 1",
  }
`, diff([]any{cmpEqualsWant, "extra line 1", "extra line 2", "extra line 3"}, []any{cmpEqualsWant, "extra line 1"})),
}, {
	about:   "CmpEquals: different values: long output and verbose",
	checker: qt.CmpEquals([]any{cmpEqualsWant, "extra line 1", "extra line 2"}, []any{cmpEqualsWant, "extra line 1"}),
	verbose: true,
	expectedCheckFailure: fmt.Sprintf(`
error:
  values are not deep equal
diff (-got +want):
%s
got:
  []interface {}{
      qt_test.cmpType{
          Strings: {
              "who",
              "dalek",
          },
          Ints: {42},
      },
      "extra line 1",
      "extra line 2",
  }
want:
  []interface {}{
      qt_test.cmpType{
          Strings: {
              "who",
              "dalek",
          },
          Ints: {42},
      },
      "extra line 1",
  }
`, diff([]any{cmpEqualsWant, "extra line 1", "extra line 2"}, []any{cmpEqualsWant, "extra line 1"})),
}, {
	about:   "CmpEquals: different values, long output, same number of lines",
	checker: qt.CmpEquals([]any{cmpEqualsWant, "extra line 1", "extra line 2", "extra line 3"}, []any{cmpEqualsWant, "extra line 1", "extra line 2", "extra line three"}),
	expectedCheckFailure: fmt.Sprintf(`
error:
  values are not deep equal
diff (-got +want):
%s
got:
  <suppressed due to length (11 lines), use -v for full output>
want:
  <suppressed due to length (11 lines), use -v for full output>
`, diff([]any{cmpEqualsWant, "extra line 1", "extra line 2", "extra line 3"}, []any{cmpEqualsWant, "extra line 1", "extra line 2", "extra line three"})),
}, {
	about:   "CmpEquals: same values with options",
	checker: qt.CmpEquals([]int{1, 2, 3}, []int{3, 2, 1}, sameInts),
	expectedNegateFailure: `
error:
  unexpected success
got:
  []int{1, 2, 3}
want:
  []int{3, 2, 1}
`,
}, {
	about:   "CmpEquals: different values with options",
	checker: qt.CmpEquals([]int{1, 2, 4}, []int{3, 2, 1}, sameInts),
	expectedCheckFailure: fmt.Sprintf(`
error:
  values are not deep equal
diff (-got +want):
%s
got:
  []int{1, 2, 4}
want:
  []int{3, 2, 1}
`, diff([]int{1, 2, 4}, []int{3, 2, 1}, sameInts)),
}, {
	about: "DeepEquals: structs with unexported fields not allowed",
	checker: qt.DeepEquals(
		struct{ answer int }{
			answer: 42,
		},
		struct{ answer int }{
			answer: 42,
		},
	),
	expectedCheckFailure: `
error:
  bad check: cannot handle unexported field at root.answer:
  	"github.com/go-quicktest/qt_test".(struct { answer int })
  consider using a custom Comparer; if you control the implementation of type, you can also consider using an Exporter, AllowUnexported, or cmpopts.IgnoreUnexported
`,
	expectedNegateFailure: `
error:
  bad check: cannot handle unexported field at root.answer:
  	"github.com/go-quicktest/qt_test".(struct { answer int })
  consider using a custom Comparer; if you control the implementation of type, you can also consider using an Exporter, AllowUnexported, or cmpopts.IgnoreUnexported
`,
}, {
	about: "CmpEquals: structs with unexported fields ignored",
	checker: qt.CmpEquals(
		struct{ answer int }{
			answer: 42,
		},
		struct{ answer int }{
			answer: 42,
		}, cmpopts.IgnoreUnexported(struct{ answer int }{})),
	expectedNegateFailure: `
error:
  unexpected success
got:
  struct { answer int }{answer:42}
want:
  <same as "got">
`,
}, {
	about:   "DeepEquals: same times",
	checker: qt.DeepEquals(goTime, goTime),
	expectedNegateFailure: `
error:
  unexpected success
got:
  s"2012-03-28 00:00:00 +0000 UTC"
want:
  <same as "got">
`,
}, {
	about:   "DeepEquals: different times: verbose",
	checker: qt.DeepEquals(goTime.Add(24*time.Hour), goTime),
	verbose: true,
	expectedCheckFailure: fmt.Sprintf(`
error:
  values are not deep equal
diff (-got +want):
%s
got:
  s"2012-03-29 00:00:00 +0000 UTC"
want:
  s"2012-03-28 00:00:00 +0000 UTC"
`, diff(goTime.Add(24*time.Hour), goTime)),
}, {
	about:   "ContentEquals: same values",
	checker: qt.ContentEquals([]string{"these", "are", "the", "voyages"}, []string{"these", "are", "the", "voyages"}),
	expectedNegateFailure: `
error:
  unexpected success
got:
  []string{"these", "are", "the", "voyages"}
want:
  <same as "got">
`,
}, {
	about:   "ContentEquals: same contents",
	checker: qt.ContentEquals([]int{1, 2, 3}, []int{3, 2, 1}),
	expectedNegateFailure: `
error:
  unexpected success
got:
  []int{1, 2, 3}
want:
  []int{3, 2, 1}
`,
}, {
	about: "ContentEquals: same contents on complex slice",
	checker: qt.ContentEquals(
		[]struct {
			Strings []any
			Ints    []int
		}{cmpEqualsGot, cmpEqualsGot, cmpEqualsWant},
		[]struct {
			Strings []any
			Ints    []int
		}{cmpEqualsWant, cmpEqualsGot, cmpEqualsGot},
	),
	expectedNegateFailure: `
error:
  unexpected success
got:
  []struct { Strings []interface {}; Ints []int }{
      {
          Strings: {
              "who",
              "dalek",
          },
          Ints: {42, 47},
      },
      {
          Strings: {
              "who",
              "dalek",
          },
          Ints: {42, 47},
      },
      {
          Strings: {
              "who",
              "dalek",
          },
          Ints: {42},
      },
  }
want:
  []struct { Strings []interface {}; Ints []int }{
      {
          Strings: {
              "who",
              "dalek",
          },
          Ints: {42},
      },
      {
          Strings: {
              "who",
              "dalek",
          },
          Ints: {42, 47},
      },
      {
          Strings: {
              "who",
              "dalek",
          },
          Ints: {42, 47},
      },
  }
`}, {
	about: "ContentEquals: same contents on a nested slice",
	checker: qt.ContentEquals(
		struct {
			Nums []int
		}{
			Nums: []int{1, 2, 3, 4},
		},
		struct {
			Nums []int
		}{
			Nums: []int{4, 3, 2, 1},
		},
	),
	expectedNegateFailure: `
error:
  unexpected success
got:
  struct { Nums []int }{
      Nums: {1, 2, 3, 4},
  }
want:
  struct { Nums []int }{
      Nums: {4, 3, 2, 1},
  }
`,
}, {
	about:   "ContentEquals: slices of different type",
	checker: qt.ContentEquals[any]([]string{"bad", "wolf"}, []any{"bad", "wolf"}),
	expectedCheckFailure: fmt.Sprintf(`
error:
  values are not deep equal
diff (-got +want):
%s
got:
  []string{"bad", "wolf"}
want:
  []interface {}{
      "bad",
      "wolf",
  }
`, diff([]string{"bad", "wolf"}, []any{"bad", "wolf"})),
}, {
	about:   "Matches: perfect match",
	checker: qt.Matches("exterminate", "exterminate"),
	expectedNegateFailure: `
error:
  unexpected success
got value:
  "exterminate"
regexp:
  <same as "got value">
`,
}, {
	about:   "Matches: match",
	checker: qt.Matches("these are the voyages", "these are the .*"),
	expectedNegateFailure: `
error:
  unexpected success
got value:
  "these are the voyages"
regexp:
  "these are the .*"
`,
}, {
	about:   "Matches: mismatch",
	checker: qt.Matches("voyages", "these are the voyages"),
	expectedCheckFailure: `
error:
  value does not match regexp
got value:
  "voyages"
regexp:
  "these are the voyages"
`,
}, {
	about:   "Matches: empty pattern",
	checker: qt.Matches("these are the voyages", ""),
	expectedCheckFailure: `
error:
  value does not match regexp
got value:
  "these are the voyages"
regexp:
  ""
`,
}, {
	about:   "Matches: complex pattern",
	checker: qt.Matches("end of the universe", "bad wolf|end of the .*"),
	expectedNegateFailure: `
error:
  unexpected success
got value:
  "end of the universe"
regexp:
  "bad wolf|end of the .*"
`,
}, {
	about:   "Matches: invalid pattern",
	checker: qt.Matches("voyages", "("),
	expectedCheckFailure: `
error:
  bad check: cannot compile regexp: error parsing regexp: missing closing ): ` + "`^(()$`" + `
regexp:
  "("
`,
	expectedNegateFailure: `
error:
  bad check: cannot compile regexp: error parsing regexp: missing closing ): ` + "`^(()$`" + `
regexp:
  "("
`,
}, {
	about:   "Matches: match with pre-compiled regexp",
	checker: qt.Matches("resistance is futile", regexp.MustCompile("resistance is (futile|useful)")),
	expectedNegateFailure: `
error:
  unexpected success
got value:
  "resistance is futile"
regexp:
  s"resistance is (futile|useful)"
`,
}, {
	about:   "Matches: mismatch with pre-compiled regexp",
	checker: qt.Matches("resistance is cool", regexp.MustCompile("resistance is (futile|useful)")),
	expectedCheckFailure: `
error:
  value does not match regexp
got value:
  "resistance is cool"
regexp:
  s"resistance is (futile|useful)"
`,
}, {
	about:   "Matches: match with pre-compiled multi-line regexp",
	checker: qt.Matches("line 1\nline 2", regexp.MustCompile(`line \d\nline \d`)),
	expectedNegateFailure: `
error:
  unexpected success
got value:
  "line 1\nline 2"
regexp:
  s"line \\d\\nline \\d"
`,
}, {
	about:   "ErrorMatches: perfect match",
	checker: qt.ErrorMatches(errBadWolf, "bad wolf"),
	expectedNegateFailure: `
error:
  unexpected success
got error:
  bad wolf
    file:line
regexp:
  "bad wolf"
`,
}, {
	about:   "ErrorMatches: match",
	checker: qt.ErrorMatches(errBadWolf, "bad .*"),
	expectedNegateFailure: `
error:
  unexpected success
got error:
  bad wolf
    file:line
regexp:
  "bad .*"
`,
}, {
	about:   "ErrorMatches: mismatch",
	checker: qt.ErrorMatches(errBadWolf, "exterminate"),
	expectedCheckFailure: `
error:
  error does not match regexp
got error:
  bad wolf
    file:line
regexp:
  "exterminate"
`,
}, {
	about:   "ErrorMatches: empty pattern",
	checker: qt.ErrorMatches(errBadWolf, ""),
	expectedCheckFailure: `
error:
  error does not match regexp
got error:
  bad wolf
    file:line
regexp:
  ""
`,
}, {
	about:   "ErrorMatches: complex pattern",
	checker: qt.ErrorMatches(errBadWolf, "bad wolf|end of the universe"),
	expectedNegateFailure: `
error:
  unexpected success
got error:
  bad wolf
    file:line
regexp:
  "bad wolf|end of the universe"
`,
}, {
	about:   "ErrorMatches: invalid pattern",
	checker: qt.ErrorMatches(errBadWolf, "("),
	expectedCheckFailure: `
error:
  bad check: cannot compile regexp: error parsing regexp: missing closing ): ` + "`^(()$`" + `
regexp:
  "("
`,
	expectedNegateFailure: `
error:
  bad check: cannot compile regexp: error parsing regexp: missing closing ): ` + "`^(()$`" + `
regexp:
  "("
`,
}, {
	about:   "ErrorMatches: nil error",
	checker: qt.ErrorMatches(nil, "some pattern"),
	expectedCheckFailure: `
error:
  got nil error but want non-nil
got error:
  nil
regexp:
  "some pattern"
`,
}, {
	about:   "ErrorMatches: match with pre-compiled regexp",
	checker: qt.ErrorMatches(errBadWolf, regexp.MustCompile("bad (wolf|dog)")),
	expectedNegateFailure: `
error:
  unexpected success
got error:
  bad wolf
    file:line
regexp:
  s"bad (wolf|dog)"
`,
}, {
	about:   "ErrorMatches: match with pre-compiled multi-line regexp",
	checker: qt.ErrorMatches(errBadWolfMultiLine, regexp.MustCompile(`bad (wolf|dog)\nfaulty (logic|statement)`)),
	expectedNegateFailure: `
error:
  unexpected success
got error:
  bad wolf
  faulty logic
    file:line
regexp:
  s"bad (wolf|dog)\\nfaulty (logic|statement)"
`,
}, {
	about:   "ErrorMatches: mismatch with pre-compiled regexp",
	checker: qt.ErrorMatches(errBadWolf, regexp.MustCompile("good (wolf|dog)")),
	expectedCheckFailure: `
error:
  error does not match regexp
got error:
  bad wolf
    file:line
regexp:
  s"good (wolf|dog)"
`,
}, {
	about:   "PanicMatches: perfect match",
	checker: qt.PanicMatches(func() { panic("error: bad wolf") }, "error: bad wolf"),
	expectedNegateFailure: `
error:
  unexpected success
panic value:
  "error: bad wolf"
function:
  func() {...}
regexp:
  <same as "panic value">
`,
}, {
	about:   "PanicMatches: match",
	checker: qt.PanicMatches(func() { panic("error: bad wolf") }, "error: .*"),
	expectedNegateFailure: `
error:
  unexpected success
panic value:
  "error: bad wolf"
function:
  func() {...}
regexp:
  "error: .*"
`,
}, {
	about:   "PanicMatches: mismatch",
	checker: qt.PanicMatches(func() { panic("error: bad wolf") }, "error: exterminate"),
	expectedCheckFailure: `
error:
  panic value does not match regexp
panic value:
  "error: bad wolf"
function:
  func() {...}
regexp:
  "error: exterminate"
`,
}, {
	about:   "PanicMatches: empty pattern",
	checker: qt.PanicMatches(func() { panic("error: bad wolf") }, ""),
	expectedCheckFailure: `
error:
  panic value does not match regexp
panic value:
  "error: bad wolf"
function:
  func() {...}
regexp:
  ""
`,
}, {
	about:   "PanicMatches: complex pattern",
	checker: qt.PanicMatches(func() { panic("bad wolf") }, "bad wolf|end of the universe"),
	expectedNegateFailure: `
error:
  unexpected success
panic value:
  "bad wolf"
function:
  func() {...}
regexp:
  "bad wolf|end of the universe"
`,
}, {
	about:   "PanicMatches: invalid pattern",
	checker: qt.PanicMatches(func() { panic("error: bad wolf") }, "("),
	expectedCheckFailure: `
error:
  bad check: cannot compile regexp: error parsing regexp: missing closing ): ` + "`^(()$`" + `
panic value:
  "error: bad wolf"
regexp:
  "("
`,
	expectedNegateFailure: `
error:
  bad check: cannot compile regexp: error parsing regexp: missing closing ): ` + "`^(()$`" + `
panic value:
  "error: bad wolf"
regexp:
  "("
`,
}, {
	about:   "PanicMatches: no panic",
	checker: qt.PanicMatches(func() {}, ".*"),
	expectedCheckFailure: `
error:
  function did not panic
function:
  func() {...}
regexp:
  ".*"
`,
}, {
	about:   "PanicMatches: match with pre-compiled regexp",
	checker: qt.PanicMatches(func() { panic("error: bad wolf") }, regexp.MustCompile("error: bad (wolf|dog)")),
	expectedNegateFailure: `
error:
  unexpected success
panic value:
  "error: bad wolf"
function:
  func() {...}
regexp:
  s"error: bad (wolf|dog)"
`,
}, {
	about:   "PanicMatches: match with pre-compiled multi-line regexp",
	checker: qt.PanicMatches(func() { panic("error: bad wolf\nfaulty logic") }, regexp.MustCompile(`error: bad (wolf|dog)\nfaulty (logic|statement)`)),
	expectedNegateFailure: `
error:
  unexpected success
panic value:
  "error: bad wolf\nfaulty logic"
function:
  func() {...}
regexp:
  s"error: bad (wolf|dog)\\nfaulty (logic|statement)"
`,
}, {
	about:   "PanicMatches: mismatch with pre-compiled regexp",
	checker: qt.PanicMatches(func() { panic("error: bad wolf") }, regexp.MustCompile("good (wolf|dog)")),
	expectedCheckFailure: `
error:
  panic value does not match regexp
panic value:
  "error: bad wolf"
function:
  func() {...}
regexp:
  s"good (wolf|dog)"
`,
}, {
	about:   "IsNil: nil",
	checker: qt.IsNil(any(nil)),
	expectedNegateFailure: `
error:
  got <nil> but want non-nil
got:
  nil
`,
}, {
	about:   "IsNil: nil pointer to struct",
	checker: qt.IsNil((*struct{})(nil)),
	expectedNegateFailure: `
error:
  got nil ptr but want non-nil
got:
  (*struct {})(nil)
`,
}, {
	about:   "IsNil: nil func",
	checker: qt.IsNil((func())(nil)),
	expectedNegateFailure: `
error:
  got nil func but want non-nil
got:
  func() {...}
`,
}, {
	about:   "IsNil: nil map",
	checker: qt.IsNil((map[string]string)(nil)),
	expectedNegateFailure: `
error:
  got nil map but want non-nil
got:
  map[string]string{}
`,
}, {
	about:   "IsNil: nil slice",
	checker: qt.IsNil(([]int)(nil)),
	expectedNegateFailure: `
error:
  got nil slice but want non-nil
got:
  []int(nil)
`,
}, {
	about:   "IsNil: nil error-implementing type",
	checker: qt.IsNil(error((*errTest)(nil))),
	// TODO e<nil> isn't great here - perhaps we should
	// mention the type too.
	expectedCheckFailure: `
error:
  got non-nil value
got:
  e<nil>
`,
}, {
	about:   "IsNil: not nil",
	checker: qt.IsNil([]int{}),
	expectedCheckFailure: `
error:
  got non-nil value
got:
  []int{}
`,
}, {
	about:   "IsNil: error is not nil",
	checker: qt.IsNil(error(errBadWolf)),
	expectedCheckFailure: `
error:
  got non-nil value
got:
  bad wolf
    file:line
`,
}, {
	about:   "IsNotNil: success",
	checker: qt.IsNotNil(any(42)),
	expectedNegateFailure: `
error:
  got non-nil value
got:
  int(42)
`,
}, {
	about:   "IsNotNil: failure",
	checker: qt.IsNotNil[any](nil),
	expectedCheckFailure: `
error:
  got <nil> but want non-nil
got:
  nil
`,
}, {
	about:   "HasLen: arrays with the same length",
	checker: qt.HasLen([4]string{"these", "are", "the", "voyages"}, 4),
	expectedNegateFailure: `
error:
  unexpected success
len(got):
  int(4)
got:
  [4]string{"these", "are", "the", "voyages"}
want length:
  <same as "len(got)">
`,
}, {
	about:   "HasLen: channels with the same length",
	checker: qt.HasLen(chInt, 2),
	expectedNegateFailure: fmt.Sprintf(`
error:
  unexpected success
len(got):
  int(2)
got:
  (chan int)(%v)
want length:
  <same as "len(got)">
`, chInt),
}, {
	about:   "HasLen: maps with the same length",
	checker: qt.HasLen(map[string]bool{"true": true}, 1),
	expectedNegateFailure: `
error:
  unexpected success
len(got):
  int(1)
got:
  map[string]bool{"true":true}
want length:
  <same as "len(got)">
`,
}, {
	about:   "HasLen: slices with the same length",
	checker: qt.HasLen([]int{}, 0),
	expectedNegateFailure: `
error:
  unexpected success
len(got):
  int(0)
got:
  []int{}
want length:
  <same as "len(got)">
`,
}, {
	about:   "HasLen: strings with the same length",
	checker: qt.HasLen("these are the voyages", 21),
	expectedNegateFailure: `
error:
  unexpected success
len(got):
  int(21)
got:
  "these are the voyages"
want length:
  <same as "len(got)">
`,
}, {
	about:   "HasLen: arrays with different lengths",
	checker: qt.HasLen([4]string{"these", "are", "the", "voyages"}, 0),
	expectedCheckFailure: `
error:
  unexpected length
len(got):
  int(4)
got:
  [4]string{"these", "are", "the", "voyages"}
want length:
  int(0)
`,
}, {
	about:   "HasLen: channels with different lengths",
	checker: qt.HasLen(chInt, 4),
	expectedCheckFailure: fmt.Sprintf(`
error:
  unexpected length
len(got):
  int(2)
got:
  (chan int)(%v)
want length:
  int(4)
`, chInt),
}, {
	about:   "HasLen: maps with different lengths",
	checker: qt.HasLen(map[string]bool{"true": true}, 42),
	expectedCheckFailure: `
error:
  unexpected length
len(got):
  int(1)
got:
  map[string]bool{"true":true}
want length:
  int(42)
`,
}, {
	about:   "HasLen: slices with different lengths",
	checker: qt.HasLen([]int{42, 47}, 1),
	expectedCheckFailure: `
error:
  unexpected length
len(got):
  int(2)
got:
  []int{42, 47}
want length:
  int(1)
`,
}, {
	about:   "HasLen: strings with different lengths",
	checker: qt.HasLen("these are the voyages", 42),
	expectedCheckFailure: `
error:
  unexpected length
len(got):
  int(21)
got:
  "these are the voyages"
want length:
  int(42)
`,
}, {
	about:   "HasLen: value without a length",
	checker: qt.HasLen(42, 42),
	expectedCheckFailure: `
error:
  bad check: first argument of type int has no length
got:
  int(42)
`,
	expectedNegateFailure: `
error:
  bad check: first argument of type int has no length
got:
  int(42)
`,
}, {
	about:   "Implements: implements interface",
	checker: qt.Implements[error](errBadWolf),
	expectedNegateFailure: `
error:
  unexpected success
got:
  bad wolf
    file:line
want interface:
  error
`,
}, {
	about:   "Implements: does not implement interface",
	checker: qt.Implements[Fooer](errBadWolf),
	expectedCheckFailure: `
error:
  got value does not implement wanted interface
got:
  bad wolf
    file:line
want interface:
  qt_test.Fooer
`,
}, {
	about:   "Implements: fails if got nil",
	checker: qt.Implements[Fooer](nil),
	expectedCheckFailure: `
error:
  got nil value but want non-nil
got:
  nil
`,
}, {
	about:   "Satisfies: success with an error",
	checker: qt.Satisfies(qt.BadCheckf("bad wolf"), qt.IsBadCheck),
	expectedNegateFailure: `
error:
  unexpected success
got:
  e"bad check: bad wolf"
predicate:
  func(error) bool {...}
`,
}, {
	about:   "Satisfies: success with an int",
	checker: qt.Satisfies(42, func(v int) bool { return v == 42 }),
	expectedNegateFailure: `
error:
  unexpected success
got:
  int(42)
predicate:
  func(int) bool {...}
`,
}, {
	about:   "Satisfies: success with nil",
	checker: qt.Satisfies([]int(nil), func(v []int) bool { return true }),
	expectedNegateFailure: `
error:
  unexpected success
got:
  []int(nil)
predicate:
  func([]int) bool {...}
`,
}, {
	about:   "Satisfies: failure with an error",
	checker: qt.Satisfies(nil, qt.IsBadCheck),
	expectedCheckFailure: `
error:
  value does not satisfy predicate function
got:
  nil
predicate:
  func(error) bool {...}
`,
}, {
	about:   "Satisfies: failure with a string",
	checker: qt.Satisfies("bad wolf", func(string) bool { return false }),
	expectedCheckFailure: `
error:
  value does not satisfy predicate function
got:
  "bad wolf"
predicate:
  func(string) bool {...}
`,
}, {
	about:   "IsTrue: success",
	checker: qt.IsTrue(true),
	expectedNegateFailure: `
error:
  unexpected success
got:
  bool(true)
want:
  <same as "got">
`,
}, {
	about:   "IsTrue: failure",
	checker: qt.IsTrue(false),
	expectedCheckFailure: `
error:
  values are not equal
got:
  bool(false)
want:
  bool(true)
`,
}, {
	about:   "IsTrue: success with subtype",
	checker: qt.IsTrue(boolean(true)),
	expectedNegateFailure: `
error:
  unexpected success
got:
  qt_test.boolean(true)
want:
  <same as "got">
`,
}, {
	about:   "IsTrue: failure with subtype",
	checker: qt.IsTrue(boolean(false)),
	expectedCheckFailure: `
error:
  values are not equal
got:
  qt_test.boolean(false)
want:
  qt_test.boolean(true)
`,
}, {
	about:   "IsFalse: success",
	checker: qt.IsFalse(false),
	expectedNegateFailure: `
error:
  unexpected success
got:
  bool(false)
want:
  <same as "got">
`,
}, {
	about:   "IsFalse: failure",
	checker: qt.IsFalse(true),
	expectedCheckFailure: `
error:
  values are not equal
got:
  bool(true)
want:
  bool(false)
`,
}, {
	about:   "StringContains match",
	checker: qt.StringContains("hello, world", "world"),
	expectedNegateFailure: `
error:
  unexpected success
got:
  "hello, world"
substr:
  "world"
`,
}, {
	about:   "StringContains no match",
	checker: qt.StringContains("hello, world", "worlds"),
	expectedCheckFailure: `
error:
  no substring match found
got:
  "hello, world"
substr:
  "worlds"
`}, {
	about:   "SliceContains match",
	checker: qt.SliceContains([]string{"a", "b", "c"}, "a"),
	expectedNegateFailure: `
error:
  unexpected success
container:
  []string{"a", "b", "c"}
want:
  "a"
`,
}, {
	about:   "SliceContains mismatch",
	checker: qt.SliceContains([]string{"a", "b", "c"}, "d"),
	expectedCheckFailure: `
error:
  no matching element found
container:
  []string{"a", "b", "c"}
want:
  "d"
`,
}, {
	about: "Contains with map",
	checker: qt.MapContains(map[string]string{
		"a": "d",
		"b": "a",
	}, "d"),
	expectedNegateFailure: `
error:
  unexpected success
container:
  map[string]string{"a":"d", "b":"a"}
want:
  "d"
`,
}, {
	about: "Contains with map and interface value",
	checker: qt.MapContains(map[string]any{
		"a": "d",
		"b": "a",
	}, "d"),
	expectedNegateFailure: `
error:
  unexpected success
container:
  map[string]interface {}{
      "a": "d",
      "b": "a",
  }
want:
  "d"
`,
}, {
	about:   "All slice equals",
	checker: qt.SliceAll([]string{"a", "a"}, qt.F2(qt.Equals[string], "a")),
	expectedNegateFailure: `
error:
  unexpected success
container:
  []string{"a", "a"}
want:
  "a"
`,
}, {
	about:   "All slice match",
	checker: qt.SliceAll([]string{"red", "blue", "green"}, qt.F2(qt.Matches[string], ".*e.*")),
	expectedNegateFailure: `
error:
  unexpected success
container:
  []string{"red", "blue", "green"}
regexp:
  ".*e.*"
`,
}, {
	about: "All nested match",
	// TODO this is a bit awkward. Is there something we could do to improve it?
	checker: qt.SliceAll([][]string{{"hello", "goodbye"}, {"red", "blue"}, {}}, func(elem []string) qt.Checker {
		return qt.SliceAll(elem, qt.F2(qt.Matches[string], ".*e.*"))
	}),
	expectedNegateFailure: `
error:
  unexpected success
container:
  [][]string{
      {"hello", "goodbye"},
      {"red", "blue"},
      {},
  }
regexp:
  ".*e.*"
`,
}, {
	about: "All nested mismatch",
	checker: qt.SliceAll([][]string{{"hello", "goodbye"}, {"black", "blue"}, {}}, func(elem []string) qt.Checker {
		return qt.SliceAll(elem, qt.F2(qt.Matches[string], ".*e.*"))
	}),
	expectedCheckFailure: `
error:
  mismatch at index 1
error:
  mismatch at index 0
error:
  value does not match regexp
first mismatched element:
  "black"
`,
}, {
	about:   "All slice mismatch",
	checker: qt.SliceAll([]string{"red", "black"}, qt.F2(qt.Matches[string], ".*e.*")),
	expectedCheckFailure: `
error:
  mismatch at index 1
error:
  value does not match regexp
first mismatched element:
  "black"
`,
}, {
	about:   "All slice mismatch with DeepEqual",
	checker: qt.SliceAll([][]string{{"a", "b"}, {"a", "c"}}, qt.F2(qt.DeepEquals[[]string], []string{"a", "b"})),
	expectedCheckFailure: fmt.Sprintf(`
error:
  mismatch at index 1
error:
  values are not deep equal
diff (-got +want):
%s
got:
  []string{"a", "c"}
want:
  []string{"a", "b"}
`, diff([]string{"a", "c"}, []string{"a", "b"})),
}, {
	about:   "All mismatch with map",
	checker: qt.MapAll(map[string]string{"a": "red", "b": "black"}, qt.F2(qt.Matches[string], ".*e.*")),
	expectedCheckFailure: `
error:
  mismatch at key "b"
error:
  value does not match regexp
first mismatched element:
  "black"
`}, {
	about:   "Any no match",
	checker: qt.SliceAny([]int{}, qt.F2(qt.Equals[int], 5)),
	expectedCheckFailure: `
error:
  no matching element found
container:
  []int{}
want:
  int(5)
`,
}, {
	about: "JSONEquals simple",
	checker: qt.JSONEquals(
		[]byte(`{"First": 47.11}`),
		&OuterJSON{
			First: 47.11,
		},
	),
	expectedNegateFailure: tilde2bq(`
error:
  unexpected success
got:
  []uint8(~{"First": 47.11}~)
want:
  &qt_test.OuterJSON{
      First:  47.11,
      Second: nil,
  }
`),
}, {
	about: "JSONEquals nested",
	checker: qt.JSONEquals(
		`{"First": 47.11, "Last": [{"First": "Hello", "Second": 42}]}`,
		&OuterJSON{
			First: 47.11,
			Second: []*InnerJSON{
				{First: "Hello", Second: 42},
			},
		},
	),
	expectedNegateFailure: tilde2bq(`
error:
  unexpected success
got:
  ~{"First": 47.11, "Last": [{"First": "Hello", "Second": 42}]}~
want:
  &qt_test.OuterJSON{
      First:  47.11,
      Second: {
          &qt_test.InnerJSON{
              First:  "Hello",
              Second: 42,
              Third:  {},
          },
      },
  }
`),
}, {
	about: "JSONEquals nested with newline",
	checker: qt.JSONEquals(
		`{"First": 47.11, "Last": [{"First": "Hello", "Second": 42},
			{"First": "World", "Third": {"F": false}}]}`,
		&OuterJSON{
			First: 47.11,
			Second: []*InnerJSON{
				{First: "Hello", Second: 42},
				{First: "World", Third: map[string]bool{
					"F": false,
				}},
			},
		},
	),
	expectedNegateFailure: `
error:
  unexpected success
got:
  "{\"First\": 47.11, \"Last\": [{\"First\": \"Hello\", \"Second\": 42},\n\t\t\t{\"First\": \"World\", \"Third\": {\"F\": false}}]}"
want:
  &qt_test.OuterJSON{
      First:  47.11,
      Second: {
          &qt_test.InnerJSON{
              First:  "Hello",
              Second: 42,
              Third:  {},
          },
          &qt_test.InnerJSON{
              First:  "World",
              Second: 0,
              Third:  {"F":false},
          },
      },
  }
`,
}, {
	about: "JSONEquals extra field",
	checker: qt.JSONEquals(
		`{"NotThere": 1}`,
		&OuterJSON{
			First: 2,
		},
	),
	expectedCheckFailure: fmt.Sprintf(`
error:
  values are not deep equal
diff (-got +want):
%s
got:
  map[string]interface {}{
      "NotThere": float64(1),
  }
want:
  map[string]interface {}{
      "First": float64(2),
  }
`, diff(map[string]any{"NotThere": 1.0}, map[string]any{"First": 2.0})),
}, {
	about:   "JSONEquals cannot unmarshal obtained value",
	checker: qt.JSONEquals([]byte(`{"NotThere": `), nil),
	expectedCheckFailure: fmt.Sprintf(tilde2bq(`
error:
  cannot unmarshal obtained contents: %s; "{\"NotThere\": "
got:
  []uint8(~{"NotThere": ~)
want:
  nil
`), mustJSONUnmarshalErr(`{"NotThere": `)),
}, {
	about:   "JSONEquals cannot marshal expected value",
	checker: qt.JSONEquals([]byte(`null`), jsonErrorMarshaler{}),
	expectedCheckFailure: `
error:
  bad check: cannot marshal expected contents: json: error calling MarshalJSON for type qt_test.jsonErrorMarshaler: qt json marshal error
`,
	expectedNegateFailure: `
error:
  bad check: cannot marshal expected contents: json: error calling MarshalJSON for type qt_test.jsonErrorMarshaler: qt json marshal error
`,
}, {
	about:   "JSONEquals with []byte",
	checker: qt.JSONEquals([]byte("null"), nil),
	expectedNegateFailure: `
error:
  unexpected success
got:
  []uint8("null")
want:
  nil
`,
}, {
	about:   "JSONEquals with RawMessage",
	checker: qt.JSONEquals([]byte("null"), json.RawMessage("null")),
	expectedNegateFailure: `
error:
  unexpected success
got:
  []uint8("null")
want:
  json.RawMessage("null")
`,
}, {
	about: "CodecEquals with bad marshal",
	checker: qt.CodecEquals(
		"null",
		nil,
		func(x any) ([]byte, error) { return []byte("bad json"), nil },
		json.Unmarshal,
	),
	expectedCheckFailure: fmt.Sprintf(`
error:
  bad check: cannot unmarshal expected contents: %s
`, mustJSONUnmarshalErr("bad json")),
	expectedNegateFailure: fmt.Sprintf(`
error:
  bad check: cannot unmarshal expected contents: %s
`, mustJSONUnmarshalErr("bad json")),
}, {
	about: "CodecEquals with options",
	checker: qt.CodecEquals(
		`["b", "z", "c", "a"]`,
		[]string{"a", "c", "z", "b"},
		json.Marshal,
		json.Unmarshal,
		cmpopts.SortSlices(func(x, y any) bool { return x.(string) < y.(string) }),
	),
	expectedNegateFailure: tilde2bq(`
error:
  unexpected success
got:
  ~["b", "z", "c", "a"]~
want:
  []string{"a", "c", "z", "b"}
`),
}, {
	about:   "ErrorAs: exact match",
	checker: qt.ErrorAs(targetErr, new(*errTarget)),
	expectedNegateFailure: `
error:
  unexpected success
got:
  e"ptr: target"
as type:
  *qt_test.errTarget
`,
}, {
	about:   "ErrorAs: wrapped match",
	checker: qt.ErrorAs(fmt.Errorf("wrapped: %w", targetErr), new(*errTarget)),
	expectedNegateFailure: `
error:
  unexpected success
got:
  e"wrapped: ptr: target"
as type:
  *qt_test.errTarget
`,
}, {
	about:   "ErrorAs: fails if nil error",
	checker: qt.ErrorAs(nil, new(*errTarget)),
	expectedCheckFailure: `
error:
  got nil error but want non-nil
got:
  nil
as type:
  *qt_test.errTarget
`,
}, {
	about:   "ErrorAs: fails if mismatch",
	checker: qt.ErrorAs(errors.New("other error"), new(*errTarget)),
	expectedCheckFailure: `
error:
  wanted type is not found in error chain
got:
  e"other error"
as type:
  *qt_test.errTarget
`,
}, {
	about:   "ErrorAs: fails if mismatch with a non-pointer error implementation",
	checker: qt.ErrorAs(errors.New("other error"), new(errTargetNonPtr)),
	expectedCheckFailure: `
error:
  wanted type is not found in error chain
got:
  e"other error"
as type:
  qt_test.errTargetNonPtr
`,
}, {
	about:   "ErrorAs: bad check if invalid as",
	checker: qt.ErrorAs(targetErr, &struct{}{}),
	expectedCheckFailure: `
error:
  bad check: errors: *target must be interface or implement error
`,
	expectedNegateFailure: `
error:
  bad check: errors: *target must be interface or implement error
`,
}, {
	about:   "ErrorIs: exact match",
	checker: qt.ErrorIs(targetErr, targetErr),
	expectedNegateFailure: `
error:
  unexpected success
got:
  e"ptr: target"
want:
  <same as "got">
`,
}, {
	about:   "ErrorIs: wrapped match",
	checker: qt.ErrorIs(fmt.Errorf("wrapped: %w", targetErr), targetErr),
	expectedNegateFailure: `
error:
  unexpected success
got:
  e"wrapped: ptr: target"
want:
  e"ptr: target"
`,
}, {
	about:   "ErrorIs: fails if nil error",
	checker: qt.ErrorIs(nil, targetErr),
	expectedCheckFailure: `
error:
  got nil error but want non-nil
got:
  nil
want:
  e"ptr: target"
`,
}, {
	about:   "ErrorIs: fails if mismatch",
	checker: qt.ErrorIs(errors.New("other error"), targetErr),
	expectedCheckFailure: `
error:
  wanted error is not found in error chain
got:
  e"other error"
want:
  e"ptr: target"
`,
}, {
	about:   "ErrorIs: nil to nil match",
	checker: qt.ErrorIs(nil, nil),
	expectedNegateFailure: `
error:
  unexpected success
got:
  nil
want:
  <same as "got">
`,
}, {
	about:   "ErrorIs: non-nil to nil mismatch",
	checker: qt.ErrorIs(targetErr, nil),
	expectedCheckFailure: `
error:
  wanted error is not found in error chain
got:
  e"ptr: target"
want:
  nil
`,
}, {
	about:   "Not: failure",
	checker: qt.Not(qt.Equals(42, 42)),
	expectedCheckFailure: `
error:
  unexpected success
got:
  int(42)
want:
  <same as "got">
`,
}, {
	about:   "Not: IsNil failure",
	checker: qt.Not(qt.IsNil[*int](nil)),
	expectedCheckFailure: `
error:
  got nil ptr but want non-nil
got:
  (*int)(nil)
`,
}}

func TestCheckers(t *testing.T) {
	original := qt.TestingVerbose
	defer func() {
		qt.TestingVerbose = original
	}()
	for _, test := range checkerTests {
		*qt.TestingVerbose = func() bool {
			return test.verbose
		}
		t.Run(test.about, func(t *testing.T) {
			tt := &testingT{}
			ok := qt.Check(tt, test.checker)
			checkResult(t, ok, tt.errorString(), test.expectedCheckFailure)
		})
		t.Run("Not "+test.about, func(t *testing.T) {
			tt := &testingT{}
			ok := qt.Check(tt, qt.Not(test.checker))
			checkResult(t, ok, tt.errorString(), test.expectedNegateFailure)
		})
	}
}

func diff(x, y any, opts ...cmp.Option) string {
	d := cmp.Diff(x, y, opts...)
	return strings.TrimSuffix(qt.Prefixf("  ", "%s", d), "\n")
}

type jsonErrorMarshaler struct{}

func (jsonErrorMarshaler) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("qt json marshal error")
}

func mustJSONUnmarshalErr(s string) error {
	var v any
	err := json.Unmarshal([]byte(s), &v)
	if err == nil {
		panic("want JSON error, got nil")
	}
	return err
}

func tilde2bq(s string) string {
	return strings.Replace(s, "~", "`", -1)
}
