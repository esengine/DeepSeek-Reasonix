// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package safemath contains overflow-safe math functions.
package safemath

import "math/bits"

// Mul3 returns (x * y * z), unless at least one argument is negative or if the
// computation overflows the int type.
func Mul3(x, y, z int) (_ int, ok bool) {
	// The following is copied from image/geom.go in std,
	// but returns an explicit ok/not-ok rather than using -1 to indicate an error.
	if x < 0 || y < 0 || z < 0 {
		return -1, false
	}
	hi, lo := bits.Mul64(uint64(x), uint64(y))
	if hi != 0 {
		return -1, false
	}
	hi, lo = bits.Mul64(lo, uint64(z))
	if hi != 0 {
		return -1, false
	}
	a := int(lo)
	if a < 0 || uint64(a) != lo {
		return -1, false
	}
	return a, true
}
