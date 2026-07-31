// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package opentype

import (
	"image"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

var (
	regular font.Face
)

func init() {
	font, err := sfnt.Parse(goregular.TTF)
	if err != nil {
		panic(err)
	}

	regular, err = NewFace(font, defaultFaceOptions())
	if err != nil {
		panic(err)
	}
}

var runeTests = []struct {
	r       rune
	advance fixed.Int26_6
	dr      image.Rectangle
}{
	{' ', 213, image.Rect(0, 0, 0, 0)},
	{'A', 512, image.Rect(0, -9, 8, 0)},
	{'Á', 512, image.Rect(0, -12, 8, 0)},
	{'Æ', 768, image.Rect(0, -9, 12, 0)},
	{'i', 189, image.Rect(0, -9, 3, 0)},
	{'x', 384, image.Rect(0, -7, 6, 0)},
}

func TestFaceGlyphAdvance(t *testing.T) {
	for _, test := range runeTests {
		got, ok := regular.GlyphAdvance(test.r)
		if !ok {
			t.Errorf("could not get glyph advance width for %q", test.r)
			continue
		}

		if got != test.advance {
			t.Errorf("%q: glyph advance width=%d. want=%d", test.r, got, test.advance)
			continue
		}
	}
}

func TestFaceGlyphBounds(t *testing.T) {
	for _, test := range runeTests {
		bounds, advance, ok := regular.GlyphBounds(test.r)
		if !ok {
			t.Errorf("could not get glyph bounds for %q", test.r)
			continue
		}

		// bounds must fit inside the draw rect.
		testFixedBounds := fixed.R(test.dr.Min.X, test.dr.Min.Y,
			test.dr.Max.X, test.dr.Max.Y)
		if !bounds.In(testFixedBounds) {
			t.Errorf("%q: glyph bounds %v must be inside %v", test.r, bounds, testFixedBounds)
			continue
		}
		if advance != test.advance {
			t.Errorf("%q: glyph advance width=%d. want=%d", test.r, advance, test.advance)
			continue
		}
	}
}

func TestFaceGlyph(t *testing.T) {
	dot := image.Pt(200, 500)
	fixedDot := fixed.P(dot.X, dot.Y)

	for _, test := range runeTests {
		dr, mask, maskp, advance, ok := regular.Glyph(fixedDot, test.r)
		if !ok {
			t.Errorf("could not get glyph for %q", test.r)
			continue
		}
		if got, want := dr, test.dr.Add(dot); got != want {
			t.Errorf("%q: glyph draw rectangle=%d. want=%d", test.r, got, want)
			continue
		}
		if got, want := mask.Bounds(), image.Rect(0, 0, dr.Dx(), dr.Dy()); got != want {
			t.Errorf("%q: glyph mask rectangle=%d. want=%d", test.r, got, want)
			continue
		}
		if maskp != (image.Point{}) {
			t.Errorf("%q: glyph maskp=%d. want=%d", test.r, maskp, image.Point{})
			continue
		}
		if advance != test.advance {
			t.Errorf("%q: glyph advance width=%d. want=%d", test.r, advance, test.advance)
			continue
		}
	}
}

func BenchmarkFaceGlyph(b *testing.B) {
	fixedDot := fixed.P(200, 500)
	r := 'A'

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _, ok := regular.Glyph(fixedDot, r)
		if !ok {
			b.Fatalf("could not get glyph for %q", r)
		}
	}
}

func TestFaceKern(t *testing.T) {
	// FIXME(sbinet) there is no kerning with gofont/goregular
	for _, test := range []struct {
		r1, r2 rune
		want   fixed.Int26_6
	}{
		{'A', 'A', 0},
		{'A', 'V', 0},
		{'V', 'A', 0},
		{'A', 'v', 0},
		{'W', 'a', 0},
		{'W', 'i', 0},
		{'Y', 'i', 0},
		{'f', '(', 0},
		{'f', 'f', 0},
		{'f', 'i', 0},
		{'T', 'a', 0},
		{'T', 'e', 0},
	} {
		got := regular.Kern(test.r1, test.r2)
		if got != test.want {
			t.Errorf("(%q, %q): glyph kerning=%d. want=%d", test.r1, test.r2, got, test.want)
			continue
		}
	}
}

func TestFaceMetrics(t *testing.T) {
	want := font.Metrics{Height: 888, Ascent: 726, Descent: 162, XHeight: 407, CapHeight: 555,
		CaretSlope: image.Point{X: 0, Y: 1}}
	got := regular.Metrics()
	if got != want {
		t.Fatalf("metrics failed. got=%#v. want=%#v", got, want)
	}
}
