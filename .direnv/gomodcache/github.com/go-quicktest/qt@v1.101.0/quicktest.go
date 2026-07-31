// Package qt implements assertions and other helpers wrapped around the
// standard library's testing types.
package qt

import (
	"testing"
)

// Assert checks that the provided argument passes the given check and calls
// tb.Fatal otherwise, including any Comment arguments in the failure.
func Assert(t testing.TB, checker Checker, comments ...Comment) bool {
	return check(t, checkParams{
		fail:     t.Fatal,
		checker:  checker,
		comments: comments,
	})
}

// Check checks that the provided argument passes the given check and calls
// tb.Error otherwise, including any Comment arguments in the failure.
func Check(t testing.TB, checker Checker, comments ...Comment) bool {
	return check(t, checkParams{
		fail:     t.Error,
		checker:  checker,
		comments: comments,
	})
}

func check(t testing.TB, p checkParams) bool {
	t.Helper()
	rp := reportParams{
		comments: p.comments,
	}

	// Allow checkers to annotate messages.
	note := func(key string, value any) {
		rp.notes = append(rp.notes, note{
			key:   key,
			value: value,
		})
	}

	// Ensure that we have a checker.
	if p.checker == nil {
		p.fail(report(BadCheckf("nil checker provided"), rp))
		return false
	}
	rp.args = p.checker.Args()

	// Run the check.
	if err := p.checker.Check(note); err != nil {
		p.fail(report(err, rp))
		return false
	}
	return true
}

type checkParams struct {
	fail     func(...any)
	checker  Checker
	comments []Comment
}
