// Licensed under the MIT license, see LICENSE file for details.

package qt_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-quicktest/qt"
)

func TestBadCheckf(t *testing.T) {
	err := qt.BadCheckf("bad %s", "wolf")
	expectedMessage := "bad check: bad wolf"
	if err.Error() != expectedMessage {
		t.Fatalf("error:\ngot  %q\nwant %q", err, expectedMessage)
	}
}

func TestIsBadCheck(t *testing.T) {
	err := qt.BadCheckf("bad wolf")
	assertBool(t, qt.IsBadCheck(err), true)
	err = errors.New("bad wolf")
	assertBool(t, qt.IsBadCheck(err), false)
}

var errBadWolf = &errTest{
	msg:       "bad wolf",
	formatted: true,
}

var errBadWolfMultiLine = &errTest{
	msg:       "bad wolf\nfaulty logic",
	formatted: true,
}

// errTest is an error type used in tests.
type errTest struct {
	msg       string
	formatted bool
}

// Error implements error.
func (err *errTest) Error() string {
	return err.msg
}

// Format implements fmt.Formatter.
func (err *errTest) Format(f fmt.State, c rune) {
	if !f.Flag('+') || c != 'v' {
		fmt.Fprint(f, "unexpected verb for formatting the error")
	}
	fmt.Fprint(f, err.Error())
	if err.formatted {
		fmt.Fprint(f, "\n  file:line")
	}
}
