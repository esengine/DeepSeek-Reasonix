// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package udiff_test

import (
	"testing"

	diff "github.com/aymanbagabas/go-udiff"
)

func TestMerge(t *testing.T) {
	// For convenience, we test Merge using strings, not sequences
	// of edits, though this does put us at the mercy of the diff
	// algorithm.
	for _, test := range []struct {
		base, x, y string
		want       string // "!" => conflict
	}{
		// independent insertions
		{"abcdef", "abXcdef", "abcdeYf", "abXcdeYf"},
		// independent deletions
		{"abcdef", "acdef", "abcdf", "acdf"},
		// colocated insertions (X first)
		{"abcdef", "abcXdef", "abcYdef", "abcXYdef"},
		// colocated identical insertions (coalesced)
		{"abcdef", "abcXdef", "abcXdef", "abcXdef"},
		// colocated insertions with common prefix (X first)
		// TODO(adonovan): would "abcXYdef" be better?
		// i.e. should we dissect the insertions?
		{"abcdef", "abcXdef", "abcXYdef", "abcXXYdef"},
		// mix of identical and independent insertions (X first)
		{"abcdef", "aIbcdXef", "aIbcdYef", "aIbcdXYef"},
		// independent deletions
		{"abcdef", "def", "abc", ""},
		// overlapping deletions: conflict
		{"abcdef", "adef", "abef", "!"},
		// overlapping deletions with distinct insertions, X first
		{"abcdef", "abXef", "abcYf", "!"},
		// overlapping deletions with distinct insertions, Y first
		{"abcdef", "abcXf", "abYef", "!"},
		// overlapping deletions with common insertions
		{"abcdef", "abXef", "abcXf", "!"},
		// trailing insertions in X (observe X bias)
		{"abcdef", "aXbXcXdXeXfX", "aYbcdef", "aXYbXcXdXeXfX"},
		// trailing insertions in Y (observe X bias)
		{"abcdef", "aXbcdef", "aYbYcYdYeYfY", "aXYbYcYdYeYfY"},
	} {
		dx := diff.Strings(test.base, test.x)
		dy := diff.Strings(test.base, test.y)
		got := "!" // conflict
		if dz, ok := diff.Merge(dx, dy); ok {
			var err error
			got, err = diff.Apply(test.base, dz)
			if err != nil {
				t.Errorf("Merge(%q, %q, %q) produced invalid edits %v: %v", test.base, test.x, test.y, dz, err)
				continue
			}
		}
		if test.want != got {
			t.Errorf("base=%q x=%q y=%q: got %q, want %q", test.base, test.x, test.y, got, test.want)
		}
	}
}
