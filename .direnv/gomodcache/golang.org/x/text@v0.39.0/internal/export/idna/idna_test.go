// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package idna

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/text/internal/testtext"
)

func TestAllocToUnicode(t *testing.T) {
	avg := testtext.AllocsPerRun(1000, func() {
		ToUnicode("www.golang.org")
	})
	if avg > 0 {
		t.Errorf("got %f; want 0", avg)
	}
}

func TestAllocToASCII(t *testing.T) {
	avg := testtext.AllocsPerRun(1000, func() {
		ToASCII("www.golang.org")
	})
	if avg > 0 {
		t.Errorf("got %f; want 0", avg)
	}
}

func TestProfiles(t *testing.T) {
	testCases := []struct {
		name      string
		want, got *Profile
	}{
		{"Punycode", punycode, New()},
		{"Registration", registration, New(ValidateForRegistration())},
		{"Registration", registration, New(
			ValidateForRegistration(),
			VerifyDNSLength(true),
			BidiRule(),
		)},
		{"Lookup", lookup, New(MapForLookup(), BidiRule(), Transitional(transitionalLookup))},
		{"Display", display, New(MapForLookup(), BidiRule())},
	}
	for _, tc := range testCases {
		// Functions are not comparable, but the printed version will include
		// their pointers.
		got := fmt.Sprintf("%#v", tc.got)
		want := fmt.Sprintf("%#v", tc.want)
		if got != want {
			t.Errorf("%s: \ngot  %#v,\nwant %#v", tc.name, got, want)
		}
	}
}

// doTest performs a single test f(input) and verifies that the output matches
// out and that the returned error is expected. The errors string contains
// all allowed error codes as categorized in
// https://www.unicode.org/Public/idna/9.0.0/IdnaTest.txt:
// P: Processing
// V: Validity
// A: to ASCII
// B: Bidi
// C: Context J
func doTest(t *testing.T, f func(string) (string, error), name, input, want, errors string) {
	errors = strings.Trim(errors, "[]")
	test := "ok"
	if errors != "" {
		test = "err:" + errors
	}
	// Replace some of the escape sequences to make it easier to single out
	// tests on the command name.
	in := strings.Trim(strconv.QuoteToASCII(input), `"`)
	in = strings.Replace(in, `\u`, "#", -1)
	in = strings.Replace(in, `\U`, "#", -1)
	name = fmt.Sprintf("%s/%s/%s", name, in, test)

	t.Run(name, func(t *testing.T) {
		got, err := f(input)

		if err != nil {
			code := err.(interface {
				code() string
			}).code()
			if strings.Index(errors, code) == -1 {
				t.Errorf("error %q not in set of expected errors {%v}", code, errors)
			}
		} else if errors != "" {
			t.Errorf("got %+q, no errors; want error in {%v}", got, errors)
		}

		if want != "" && got != want {
			t.Errorf(`input=%+q string: got %+q; want %+q`, input, got, want)
		}
	})
}

var unescapeRE = regexp.MustCompile(`\\u([0-9a-zA-Z]{4})`)

func unescape(s string) string {
	return unescapeRE.ReplaceAllStringFunc(s, func(v string) string {
		var d [2]byte
		hex.Decode(d[:], []byte(v[2:]))
		return string(rune(d[0])<<8 | rune(d[1]))
	})
}

func BenchmarkProfile(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Lookup.ToASCII("www.yahoogle.com")
	}
}

// TestLabelErrors tests strings returned in case of error. All results should
// be identical to the reference implementation and can be verified at
// https://unicode.org/cldr/utility/idna.jsp. The reference implementation,
// however, seems to not display Bidi and ContextJ errors.
//
// In some cases the behavior of browsers is added as a comment. In all cases,
// whenever a resolve search returns an error here, Chrome will treat the input
// string as a search string (including those for Bidi and Context J errors),
// unless noted otherwise.
func TestLabelErrors(t *testing.T) {
	encode := func(s string) string { s, _ = encode(acePrefix, s); return s }
	type kind struct {
		name string
		f    func(string) (string, error)
	}
	punyA := kind{"PunycodeA", punycode.ToASCII}
	resolve := kind{"ResolveA", Lookup.ToASCII}
	display := kind{"ToUnicode", Display.ToUnicode}
	p := New(VerifyDNSLength(true), MapForLookup(), BidiRule())
	lengthU := kind{"CheckLengthU", p.ToUnicode}
	lengthA := kind{"CheckLengthA", p.ToASCII}
	p = New(MapForLookup(), StrictDomainName(false))
	std3 := kind{"STD3", p.ToASCII}
	p = New(MapForLookup(), CheckHyphens(false))
	hyphens := kind{"CheckHyphens", p.ToASCII}
	p = New(MapForLookup(), Transitional(true))
	transitional := kind{"Transitional", p.ToASCII}
	p = New(MapForLookup(), Transitional(false))
	nontransitional := kind{"Nontransitional", p.ToASCII}

	testCases := []struct {
		kind
		input   string
		want    string
		wantErr string
	}{
		{lengthU, "", "", code16("A4", "X4_2")}, // From UTS 46 conformance test.
		{lengthA, "", "", "A4"},

		{lengthU, "xn--", "", code16("A4", "X4_2")},
		{lengthU, "foo.xn--", "foo.", code16("A4", "X4_2")}, // TODO: is dropping xn-- correct?
		{lengthU, "xn--.foo", ".foo", code16("A4", "X4_2")},
		{lengthU, "foo.xn--.bar", "foo..bar", code16("A4", "X4_2")},

		{display, "xn--", "", ""},
		{display, "foo.xn--", "foo.", ""}, // TODO: is dropping xn-- correct?
		{display, "xn--.foo", ".foo", ""},
		{display, "foo.xn--.bar", "foo..bar", ""},

		{lengthA, "a..b", "a..b", "A4"},
		{punyA, ".b", ".b", ""},
		// For backwards compatibility, the Punycode profile does not map runes.
		{punyA, "\u3002b", "xn--b-83t", ""},
		{punyA, "..b", "..b", ""},

		{lengthA, ".b", ".b", "A4"},
		{lengthA, "\u3002b", ".b", "A4"},
		{lengthA, "..b", "..b", "A4"},
		{lengthA, "b..", "b..", code16("", "A4")},

		// Sharpened Bidi rules for Unicode 10.0.0. Apply for ALL labels in ANY
		// of the labels is RTL.
		{lengthA, "\ufe05\u3002\u3002\U0002603e\u1ce0", "..xn--t6f5138v", "A4"},
		{lengthA, "FAX\u2a77\U0001d186\u3002\U0001e942\U000e0181\u180c", "", "B6"},

		{resolve, "a..b", "a..b", ""},
		// Note that leading dots are not stripped. This is to be consistent
		// with the Punycode profile as well as the conformance test.
		{resolve, ".b", ".b", ""},
		{resolve, "\u3002b", ".b", ""},
		{resolve, "..b", "..b", ""},
		{resolve, "b..", "b..", ""},
		{resolve, "\xed", "", "P1"},

		// Raw punycode
		{punyA, "", "", ""},
		{punyA, "*.foo.com", "*.foo.com", ""},
		{punyA, "Foo.com", "Foo.com", ""},

		// STD3 rules
		{display, "*.foo.com", "*.foo.com", code16("P1", "U1")},
		{std3, "*.foo.com", "*.foo.com", ""},

		// Hyphens
		{display, "r3---sn-apo3qvuoxuxbt-j5pe.googlevideo.com", "r3---sn-apo3qvuoxuxbt-j5pe.googlevideo.com", "V2"},
		{hyphens, "r3---sn-apo3qvuoxuxbt-j5pe.googlevideo.com", "r3---sn-apo3qvuoxuxbt-j5pe.googlevideo.com", ""},
		{display, "-label-.com", "-label-.com", "V3"},
		{hyphens, "-label-.com", "-label-.com", ""},

		// Don't map U+2490 (DIGIT NINE FULL STOP). This is the behavior of
		// Chrome, modern Firefox, Safari, and IE.
		{resolve, "lab⒐be", "xn--labbe-zh9b", code16("P1", "V7")}, // encode("lab⒐be")
		{display, "lab⒐be", "lab⒐be", code16("P1", "V7")},
		{transitional, "plan⒐faß.de", "xn--planfass-c31e.de", code16("P1", "V7")}, // encode("plan⒐fass") + ".de"
		{display, "Plan⒐faß.de", "plan⒐faß.de", code16("P1", "V7")},

		// Transitional vs Nontransitional processing
		{transitional, "Plan9faß.de", "plan9fass.de", ""},
		{nontransitional, "Plan9faß.de", "xn--plan9fa-6va.de", ""},

		// Chrome 54.0 recognizes the error and treats this input verbatim as a
		// search string.
		// Safari 10.0 (non-conform spec) decomposes "⒈" and computes the
		// punycode on the result using transitional mapping.
		// Firefox 49.0.1 goes haywire on this string and prints a bunch of what
		// seems to be nested punycode encodings.
		{transitional, "日本⒈co.ßßß.de", "xn--co-wuw5954azlb.ssssss.de", code16("P1", "V7")},
		{display, "日本⒈co.ßßß.de", "日本⒈co.ßßß.de", code16("P1", "V7")},

		{transitional, "a\u200Cb", "ab", ""},
		{display, "a\u200Cb", "a\u200Cb", "C"},

		{resolve, encode("a\u200Cb"), encode("a\u200Cb"), "C"},
		{display, "a\u200Cb", "a\u200Cb", "C"},

		{resolve, "grﻋﺮﺑﻲ.de", "xn--gr-gtd9a1b0g.de", "B"},
		{
			// Notice how the string gets transformed, even with an error.
			// Chrome will use the original string if it finds an error, so not
			// the transformed one.
			display,
			"gr\ufecb\ufeae\ufe91\ufef2.de",
			"gr\u0639\u0631\u0628\u064a.de",
			"B",
		},

		{resolve, "\u0671.\u03c3\u07dc", "xn--qib.xn--4xa21s", "B"}, // ٱ.σߜ
		{display, "\u0671.\u03c3\u07dc", "\u0671.\u03c3\u07dc", "B"},

		// normalize input
		{resolve, "a\u0323\u0322", "xn--jta191l", ""}, // ạ̢
		{display, "a\u0323\u0322", "\u1ea1\u0322", ""},

		// Non-normalized strings are not normalized when they originate from
		// punycode. Despite the error, Chrome, Safari and Firefox will attempt
		// to look up the input punycode.
		{resolve, encode("a\u0323\u0322") + ".com", "xn--a-tdbc.com", "V1"},
		{display, encode("a\u0323\u0322") + ".com", "a\u0323\u0322.com", "V1"},
	}

	for _, tc := range testCases {
		doTest(t, tc.f, tc.name, tc.input, tc.want, tc.wantErr)
	}
}

func TestTransitionalDefault(t *testing.T) {
	want := "xn--strae-oqa.de"
	if transitionalLookup {
		want = "strasse.de"
	}
	doTest(t, Lookup.ToASCII, "Lookup", "straße.de", want, "")
}
