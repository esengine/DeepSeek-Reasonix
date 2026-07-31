package graphemes_test

import (
	"bytes"
	mathrand "math/rand"
	"testing"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/graphemes"
	"github.com/clipperhouse/uax29/v2/testdata"
)

// FuzzValidShort fuzzes small, valid UTF8 strings. I suspect more, shorter
// strings in the corpus lead to more mutation and coverage. True?
func FuzzValidShort(f *testing.F) {
	if testing.Short() {
		f.Skip("skipping fuzz test in short mode")
	}
	// unicode test suite
	for _, test := range unicodeTests {
		f.Add(test.input)
	}

	// multi-lingual text, as small-ish lines
	file, err := testdata.Sample()
	if err != nil {
		f.Error(err)
	}
	lines := bytes.Split(file, []byte("\n"))
	for _, line := range lines {
		f.Add(line)
	}

	f.Fuzz(func(t *testing.T, original []byte) {
		var all [][]byte
		valid1 := utf8.Valid(original)
		tokens := graphemes.FromBytes(original)
		for tokens.Next() {
			all = append(all, tokens.Value())
		}

		roundtrip := make([]byte, 0, len(original))
		for _, s := range all {
			roundtrip = append(roundtrip, s...)
		}

		if !bytes.Equal(roundtrip, original) {
			t.Error("bytes did not roundtrip")
		}

		valid2 := utf8.Valid(roundtrip)

		if valid1 != valid2 {
			t.Error("utf8 validity of original did not match roundtrip")
		}
	})
}

// FuzzValidLong fuzzes longer, valid UTF8 strings.
func FuzzValidLong(f *testing.F) {
	if testing.Short() {
		f.Skip("skipping fuzz test in short mode")
	}
	// add multi-lingual text, as decent (paragraph-sized) size chunks
	file, err := testdata.Sample()
	if err != nil {
		f.Error(err)
	}
	chunks := bytes.Split(file, []byte("\n\n\n"))
	for _, chunk := range chunks {
		f.Add(chunk)
	}

	f.Fuzz(func(t *testing.T, original []byte) {
		var all [][]byte
		valid1 := utf8.Valid(original)
		tokens := graphemes.FromBytes(original)
		for tokens.Next() {
			all = append(all, tokens.Value())
		}

		roundtrip := make([]byte, 0, len(original))
		for _, s := range all {
			roundtrip = append(roundtrip, s...)
		}

		if !bytes.Equal(roundtrip, original) {
			t.Error("bytes did not roundtrip")
		}

		valid2 := utf8.Valid(roundtrip)

		if valid1 != valid2 {
			t.Error("utf8 validity of original did not match roundtrip")
		}
	})
}

// FuzzInvalid fuzzes invalid UTF8 strings.
func FuzzInvalid(f *testing.F) {
	if testing.Short() {
		f.Skip("skipping fuzz test in short mode")
	}
	random := getRandomBytes()

	const max = 100
	const min = 1

	pos := 0
	for {
		// random smaller strings
		ln := mathrand.Intn(max-min) + min

		if pos+ln > len(random) {
			break
		}

		f.Add(random[pos : pos+ln])
		pos += ln
	}

	// known invalid utf-8
	badUTF8, err := testdata.InvalidUTF8()
	if err != nil {
		f.Error(err)
	}
	lines := bytes.Split(badUTF8, []byte("\n"))
	for _, line := range lines {
		f.Add(line)
	}

	f.Fuzz(func(t *testing.T, original []byte) {
		var all [][]byte
		valid1 := utf8.Valid(original)
		tokens := graphemes.FromBytes(original)
		for tokens.Next() {
			all = append(all, tokens.Value())
		}

		roundtrip := make([]byte, 0, len(original))
		for _, s := range all {
			roundtrip = append(roundtrip, s...)
		}

		if !bytes.Equal(roundtrip, original) {
			t.Error("bytes did not roundtrip")
		}

		valid2 := utf8.Valid(roundtrip)

		if valid1 != valid2 {
			t.Error("utf8 validity of original did not match roundtrip")
		}
	})
}

// FuzzANSIOptions fuzzes iterator roundtripping with ANSI options enabled.
// This specifically exercises 7-bit only, 8-bit only, and combined modes.
func FuzzANSIOptions(f *testing.F) {
	if testing.Short() {
		f.Skip("skipping fuzz test in short mode")
	}

	seeds := [][]byte{
		[]byte("\x1b[31mhello\x1b[0m"),            // 7-bit CSI
		[]byte("\x1b]0;Title\x07"),                // 7-bit OSC + BEL
		[]byte("\x1bPqpayload\x1b\\"),             // 7-bit DCS + 7-bit ST
		[]byte("\x9B31mhello"),                    // C1 CSI
		[]byte("\x9D0;Title\x9C"),                 // C1 OSC + C1 ST
		[]byte("\x90qpayload\x9C"),                // C1 DCS + C1 ST
		[]byte("\x98hello\x9C"),                   // C1 SOS + C1 ST
		[]byte("\x9Emsg\x9C"),                     // C1 PM + C1 ST
		[]byte("\x9Fdata\x9C"),                    // C1 APC + C1 ST
		[]byte("\x1b]0;Title\x9C"),                // 7-bit initiator + C1 ST (strict negative)
		[]byte("\x9D0;Title\x1b\\"),               // C1 initiator + 7-bit ST (strict negative)
		[]byte("\x1b]0;本\x07"),                    // UTF-8 in OSC payload
		[]byte("\x90q本\x9C"),                     // UTF-8 in C1 DCS payload
		[]byte("\x1b[31m\x9B1;32mtext\x1b[0m"),    // mixed 7-bit + 8-bit CSI
		[]byte("\x1b"),                            // truncated ESC
		[]byte("\x9D0;unterminated"),              // unterminated C1 OSC
		[]byte("plain UTF-8: café 日本語 👩🏽‍💻"), // non-ANSI UTF-8
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, original []byte) {
		validOriginal := utf8.Valid(original)

		modes := []struct {
			name     string
			ansi7Bit bool
			ansi8Bit bool
		}{
			{name: "off", ansi7Bit: false, ansi8Bit: false},
			{name: "7bit", ansi7Bit: true, ansi8Bit: false},
			{name: "8bit", ansi7Bit: false, ansi8Bit: true},
			{name: "both", ansi7Bit: true, ansi8Bit: true},
		}

		for _, mode := range modes {
			tokens := graphemes.FromBytes(original)
			tokens.AnsiEscapeSequences = mode.ansi7Bit
			tokens.AnsiEscapeSequences8Bit = mode.ansi8Bit

			var all [][]byte
			for tokens.Next() {
				all = append(all, tokens.Value())
			}

			roundtrip := make([]byte, 0, len(original))
			for _, s := range all {
				roundtrip = append(roundtrip, s...)
			}

			if !bytes.Equal(roundtrip, original) {
				t.Fatalf("%s mode: bytes did not roundtrip", mode.name)
			}

			if validOriginal != utf8.Valid(roundtrip) {
				t.Fatalf("%s mode: utf8 validity of original did not match roundtrip", mode.name)
			}
		}
	})
}
