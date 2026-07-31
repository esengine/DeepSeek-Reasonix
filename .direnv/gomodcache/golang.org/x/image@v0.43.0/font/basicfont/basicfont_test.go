// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package basicfont

import (
	"image"
	"testing"

	"golang.org/x/image/font"
)

func TestMetrics(t *testing.T) {
	want := font.Metrics{Height: 832, Ascent: 704, Descent: 128, XHeight: 704, CapHeight: 704, CaretSlope: image.Point{X: 0, Y: 1}}
	if got := Face7x13.Metrics(); got != want {
		t.Errorf("Face7x13: Metrics: got %v want %v", got, want)
	}
}
