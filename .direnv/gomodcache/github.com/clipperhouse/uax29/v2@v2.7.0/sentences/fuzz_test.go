package sentences_test

import (
	"bytes"
	mathrand "math/rand"
	"testing"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/sentences"
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
		tokens := sentences.FromBytes(original)
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
		tokens := sentences.FromBytes(original)
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
		tokens := sentences.FromBytes(original)
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
