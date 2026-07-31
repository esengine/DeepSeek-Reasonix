// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build go1.27

package idna

import (
	"testing"

	"golang.org/x/text/internal/gen"
	"golang.org/x/text/internal/testtext"
	"golang.org/x/text/internal/ucd"
)

func TestConformance(t *testing.T) {
	testtext.SkipIfNotLong(t)

	r := gen.OpenUnicodeFile("", "", "idna/IdnaTestV2.txt")
	defer r.Close()

	section := "main"
	p := ucd.New(r)
	transitional := New(Transitional(true), VerifyDNSLength(true), BidiRule(), MapForLookup())
	nonTransitional := New(VerifyDNSLength(true), BidiRule(), MapForLookup())
	for p.Next() {
		var (
			src          = def(unescape(p.String(0)), "")
			toUnicode    = def(unescape(p.String(1)), src)
			toUnicodeErr = p.String(2)
			toASCIIN     = def(unescape(p.String(3)), toUnicode)
			toASCIINErr  = def(p.String(4), toUnicodeErr)
			toASCIIT     = def(unescape(p.String(5)), toASCIIN)
			toASCIITErr  = def(p.String(6), toASCIINErr)
		)
		doTest(t, nonTransitional.ToUnicode, section+":ToUnicode", src, toUnicode, toUnicodeErr)
		doTest(t, nonTransitional.ToASCII, section+":ToASCII:N", src, toASCIIN, toASCIINErr)
		doTest(t, transitional.ToASCII, section+":ToASCII:T", src, toASCIIT, toASCIITErr)
	}
}

func def(field, fallback string) string {
	if field == "" {
		return fallback
	}
	if field == `""` {
		return ""
	}
	return field
}
