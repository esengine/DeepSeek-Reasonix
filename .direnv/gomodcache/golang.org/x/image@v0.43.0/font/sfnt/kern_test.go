// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sfnt

/*
This file contains opt-in tests for kerning in user provided fonts.

Kerning information in kern and GPOS tables can be quite complex. These tests
recursively load all fonts from -bulkFontDirs and try to kern all possible
glyph pairs.

These tests only check if there are no errors during kerning. Tests of actual
kerning values are in proprietary_test.go.

Note: CJK fonts can contain billions of possible kerning pairs. Testing for
these fonts stops after -bulkMaxKernPairs.

To opt-in:

go test golang.org/x/image/font/sfnt -test.run=BulkKern -args -bulk -bulkFontDirs /Library/Fonts:./myfonts
*/

import (
	"flag"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

var (
	bulk = flag.Bool("bulk", false, "")

	fontDirs = flag.String(
		"bulkFontDirs",
		"./",
		"separated directories to search for fonts",
	)
	maxKernPairs = flag.Int(
		"bulkMaxKernPairs",
		20000000,
		"skip testing of kerning after this many tested pairs",
	)
)

func TestBulkKern(t *testing.T) {
	if !*bulk {
		t.Skip("skipping bulk font test")
	}

	for _, fontDir := range filepath.SplitList(*fontDirs) {
		err := filepath.Walk(fontDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, ".ttf") || strings.HasSuffix(path, ".otf") {
				t.Run(info.Name(), testFontKerning(filepath.Join(path)))
			}
			return nil
		})
		if err != nil {
			t.Fatal("error finding fonts", err)
		}
	}

}

func testFontKerning(fname string) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()
		b, err := ioutil.ReadFile(fname)
		if err != nil {
			t.Fatal(err)
		}
		fnt, err := Parse(b)
		if err != nil {
			t.Fatal(err)
		}

		buf := &Buffer{}

		// collect all GlyphIndex
		glyphs := make([]GlyphIndex, 1, fnt.NumGlyphs())
		glyphs[0] = GlyphIndex(0)
		r := rune(0)
		for r < 0xffff {
			g, err := fnt.GlyphIndex(buf, r)
			r++
			if g == 0 || err == ErrNotFound {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			glyphs = append(glyphs, g)
			if len(glyphs) == fnt.NumGlyphs() {
				break
			}
		}

		var kerned, tested int
		for _, g1 := range glyphs {
			for _, g2 := range glyphs {
				if tested >= *maxKernPairs {
					log.Printf("stop testing after %d or %d kerning pairs (found %d pairs)",
						tested, len(glyphs)*len(glyphs), kerned)
					return
				}

				tested++
				adv, err := fnt.Kern(buf, g1, g2, fixed.I(20), font.HintingNone)
				if err == ErrNotFound {
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				if adv != 0 {
					kerned++
				}
			}
		}

		log.Printf("found %d kerning pairs for %d glyphs (%.1f%%) in %q",
			kerned,
			len(glyphs),
			100*float64(kerned)/float64(len(glyphs)*len(glyphs)),
			fname,
		)
	}
}
