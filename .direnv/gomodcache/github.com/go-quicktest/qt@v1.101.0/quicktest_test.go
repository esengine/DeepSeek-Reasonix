// Licensed under the MIT license, see LICENCE file for details.

package qt_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
)

var qtTests = []struct {
	about           string
	checker         qt.Checker
	comments        []qt.Comment
	expectedFailure string
}{{
	about:   "success",
	checker: qt.Equals(42, 42),
}, {
	about:   "failure",
	checker: qt.Equals("42", "47"),
	expectedFailure: `
error:
  values are not equal
got:
  "42"
want:
  "47"
`,
}, {
	about:   "failure with % signs",
	checker: qt.Equals("42%x", "47%y"),
	expectedFailure: `
error:
  values are not equal
got:
  "42%x"
want:
  "47%y"
`,
}, {
	about:    "failure with comment",
	checker:  qt.Equals(true, false),
	comments: []qt.Comment{qt.Commentf("apparently %v != %v", true, false)},
	expectedFailure: `
error:
  values are not equal
comment:
  apparently true != false
got:
  bool(true)
want:
  bool(false)
`,
}, {
	about:    "another failure with comment",
	checker:  qt.IsNil(any(42)),
	comments: []qt.Comment{qt.Commentf("bad wolf: %d", 42)},
	expectedFailure: `
error:
  got non-nil value
comment:
  bad wolf: 42
got:
  int(42)
`,
}, {
	about:    "failure with constant comment",
	checker:  qt.IsNil(any("something")),
	comments: []qt.Comment{qt.Commentf("these are the voyages")},
	expectedFailure: `
error:
  got non-nil value
comment:
  these are the voyages
got:
  "something"
`,
}, {
	about:    "failure with empty comment",
	checker:  qt.IsNil(any(47)),
	comments: []qt.Comment{qt.Commentf("")},
	expectedFailure: `
error:
  got non-nil value
got:
  int(47)
`,
}, {
	about:   "failure with multiple comments",
	checker: qt.IsNil(any(42)),
	comments: []qt.Comment{
		qt.Commentf("bad wolf: %d", 42),
		qt.Commentf("second comment"),
	},
	expectedFailure: `
error:
  got non-nil value
comment:
  bad wolf: 42
comment:
  second comment
got:
  int(42)
`,
}, {
	about: "nil checker",
	expectedFailure: `
error:
  bad check: nil checker provided
`,
}, {
	about: "many arguments and notes",
	checker: &testingChecker{
		args: []qt.Arg{{
			Name:  "arg1",
			Value: 42,
		}, {
			Name:  "arg2",
			Value: "val2",
		}, {
			Name:  "arg3",
			Value: "val3",
		}},
		addNotes: func(note func(key string, value any)) {
			note("note1", "these")
			note("note2", qt.Unquoted("are"))
			note("note3", "the")
			note("note4", "voyages")
			note("note5", true)
		},
		err: errors.New("bad wolf"),
	},
	expectedFailure: `
error:
  bad wolf
note1:
  "these"
note2:
  are
note3:
  "the"
note4:
  "voyages"
note5:
  bool(true)
arg1:
  int(42)
arg2:
  "val2"
arg3:
  "val3"
`,
}, {
	about: "many arguments and notes with the same value",
	checker: &testingChecker{
		args: []qt.Arg{{
			Name:  "arg1",
			Value: "value1",
		}, {
			Name:  "arg2",
			Value: "value1",
		}, {
			Name:  "arg3",
			Value: []int{42},
		}, {
			Name:  "arg4",
			Value: nil,
		}},
		addNotes: func(note func(key string, value any)) {
			note("note1", "value1")
			note("note2", []int{42})
			note("note3", "value1")
			note("note4", nil)
		},
		err: errors.New("bad wolf"),
	},
	expectedFailure: `
error:
  bad wolf
note1:
  "value1"
note2:
  []int{42}
note3:
  <same as "note1">
note4:
  nil
arg1:
  <same as "note1">
arg2:
  <same as "note1">
arg3:
  <same as "note2">
arg4:
  <same as "note4">
`,
}, {
	about: "bad check with notes",
	checker: &testingChecker{
		args: []qt.Arg{{
			Name:  "got",
			Value: 42,
		}, {
			Name:  "want",
			Value: "want",
		}},
		addNotes: func(note func(key string, value any)) {
			note("note", 42)
		},
		err: qt.BadCheckf("bad wolf"),
	},
	expectedFailure: `
error:
  bad check: bad wolf
note:
  int(42)
`,
}, {
	about: "silent failure with notes",
	checker: &testingChecker{
		args: []qt.Arg{{
			Name:  "got",
			Value: 42,
		}, {
			Name:  "want",
			Value: "want",
		}},
		addNotes: func(note func(key string, value any)) {
			note("note1", "first note")
			note("note2", qt.Unquoted("second note"))
		},
		err: qt.ErrSilent,
	},
	expectedFailure: `
note1:
  "first note"
note2:
  second note
`,
}}

func TestCAssertCheck(t *testing.T) {
	for _, test := range qtTests {
		t.Run("Assert: "+test.about, func(t *testing.T) {
			tt := &testingT{}
			ok := qt.Assert(tt, test.checker, test.comments...)
			checkResult(t, ok, tt.fatalString(), test.expectedFailure)
			if tt.errorString() != "" {
				t.Fatalf("no error messages expected, but got %q", tt.errorString())
			}
		})
		t.Run("Check: "+test.about, func(t *testing.T) {
			tt := &testingT{}
			ok := qt.Check(tt, test.checker, test.comments...)
			checkResult(t, ok, tt.errorString(), test.expectedFailure)
			if tt.fatalString() != "" {
				t.Fatalf("no fatal messages expected, but got %q", tt.fatalString())
			}
		})

	}
}

func checkResult(t *testing.T, ok bool, got, want string) {
	t.Helper()
	if want != "" {
		assertPrefix(t, got, want+"stack:\n")
		assertBool(t, ok, false)
		return
	}
	if got != "" {
		t.Fatalf("output:\ngot  %q\nwant empty", got)
	}
	assertBool(t, ok, true)
}

// testingT implements testing.TB for testing purposes.
type testingT struct {
	testing.TB

	errorBuf bytes.Buffer
	fatalBuf bytes.Buffer

	subTestResult bool
	subTestName   string
	subTestT      *testing.T

	helperCalls int
	parallel    bool
}

// Error overrides testing.TB.Error so that messages are collected.
func (t *testingT) Error(a ...any) {
	fmt.Fprint(&t.errorBuf, a...)
}

// Fatal overrides testing.TB.Fatal so that messages are collected and the
// goroutine is not killed.
func (t *testingT) Fatal(a ...any) {
	fmt.Fprint(&t.fatalBuf, a...)
}

// Parallel overrides testing.TB.Parallel in order to record the call.
func (t *testingT) Parallel() {
	t.parallel = true
}

// Helper overrides testing.TB.Helper in order to count calls.
func (t *testingT) Helper() {
	t.helperCalls += 1
}

// Fatal overrides *testing.T.Fatal so that messages are collected and the
// goroutine is not killed.
func (t *testingT) Run(name string, f func(t *testing.T)) bool {
	t.subTestName, t.subTestT = name, &testing.T{}
	f(t.subTestT)
	return t.subTestResult
}

// errorString returns the error message.
func (t *testingT) errorString() string {
	return t.errorBuf.String()
}

// fatalString returns the fatal error message.
func (t *testingT) fatalString() string {
	return t.fatalBuf.String()
}

// assertPrefix fails if the got value does not have the given prefix.
func assertPrefix(t testing.TB, got, prefix string) {
	t.Helper()
	if prefix == "" {
		t.Fatal("prefix: empty value provided")
	}
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf(`prefix:
got  %q
want %q
-------------------- got --------------------
%s
-------------------- want -------------------
%s
---------------------------------------------`, got, prefix, got, prefix)
	}
}

// assertBool fails if the given boolean values don't match.
func assertBool(t testing.TB, got, want bool) {
	t.Helper()
	if got != want {
		t.Fatalf("bool:\ngot  %v\nwant %v", got, want)
	}
}

// testingChecker is a quicktest.Checker used in tests. It receives the
// provided argNames, adds notes via the provided addNotes function, and when
// the check is run the provided error is returned.
type testingChecker struct {
	args     []qt.Arg
	addNotes func(note func(key string, value any))
	err      error
}

// Check implements quicktest.Checker by returning the stored error.
func (c *testingChecker) Check(note func(key string, value any)) error {
	if c.addNotes != nil {
		c.addNotes(note)
	}
	return c.err
}

// Args implements quicktest.Checker by returning the stored args.
func (c *testingChecker) Args() []qt.Arg {
	return c.args
}
