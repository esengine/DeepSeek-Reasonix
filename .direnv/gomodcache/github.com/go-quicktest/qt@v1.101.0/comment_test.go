// Licensed under the MIT license, see LICENSE file for details.

package qt_test

import (
	"testing"

	"github.com/go-quicktest/qt"
)

func TestCommentf(t *testing.T) {
	c := qt.Commentf("the answer is %d", 42)
	comment := c.String()
	expectedComment := "the answer is 42"
	if comment != expectedComment {
		t.Fatalf("comment error:\ngot  %q\nwant %q", comment, expectedComment)
	}
}

func TestConstantCommentf(t *testing.T) {
	const expectedComment = "bad wolf"
	c := qt.Commentf(expectedComment)
	comment := c.String()
	if comment != expectedComment {
		t.Fatalf("constant comment error:\ngot  %q\nwant %q", comment, expectedComment)
	}
}
