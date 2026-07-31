// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package plan9font

import (
	"image"
	"io/ioutil"
	"path"
	"path/filepath"
	"testing"

	"golang.org/x/image/font"
)

func TestMetrics(t *testing.T) {
	readFile := func(name string) ([]byte, error) {
		return ioutil.ReadFile(filepath.FromSlash(path.Join("../testdata/fixed", name)))
	}
	data, err := readFile("unicode.7x13.font")
	if err != nil {
		t.Fatal(err)
	}
	face, err := ParseFont(data, readFile)
	if err != nil {
		t.Fatal(err)
	}
	want := font.Metrics{Height: 832, Ascent: 704, Descent: 128, XHeight: 704, CapHeight: 704,
		CaretSlope: image.Point{X: 0, Y: 1}}
	if got := face.Metrics(); got != want {
		t.Errorf("unicode.7x13.font: Metrics: got %v, want %v", got, want)
	}
	subData, err := readFile("7x13.0000")
	if err != nil {
		t.Fatal(err)
	}
	subFace, err := ParseSubfont(subData, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := subFace.Metrics(); got != want {
		t.Errorf("7x13.0000: Metrics: got %v, want %v", got, want)
	}
}

func BenchmarkParseSubfont(b *testing.B) {
	subfontData, err := ioutil.ReadFile(filepath.FromSlash("../testdata/fixed/7x13.0000"))
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ParseSubfont(subfontData, 0); err != nil {
			b.Fatal(err)
		}
	}
}
