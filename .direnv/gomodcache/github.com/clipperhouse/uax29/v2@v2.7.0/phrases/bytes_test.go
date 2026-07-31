package phrases_test

import (
	"bytes"
	"testing"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/phrases"
	"github.com/clipperhouse/uax29/v2/testdata"
)

func TestBytesRoundtrip(t *testing.T) {
	t.Parallel()

	const runs = 100

	tokens := phrases.FromBytes(nil)

	for i := 0; i < runs; i++ {
		input := getRandomBytes()
		tokens.SetText(input)

		var output []byte
		for tokens.Next() {
			output = append(output, tokens.Value()...)
		}

		if !bytes.Equal(output, input) {
			t.Fatal("input bytes are not the same as output bytes")
		}
	}
}

func TestBytesInvalidUTF8(t *testing.T) {
	t.Parallel()

	// For background, see internal/testdata/UTF-8-test.txt, or:
	// https://www.cl.cam.ac.uk/~mgk25/ucs/examples/UTF-8-test.txt

	// Btw, don't edit UTF-8-test.txt: your editor might turn it into valid UTF-8!

	input, err := testdata.InvalidUTF8()
	if err != nil {
		t.Error(err)
	}

	if utf8.Valid(input) {
		t.Error("input file should not be valid utf8")
	}

	tokens := phrases.FromBytes(input)

	var output []byte
	for tokens.Next() {
		output = append(output, tokens.Value()...)
	}

	if !bytes.Equal(output, input) {
		t.Fatalf("input bytes are not the same as output bytes")
	}
}

var exists = struct{}{}

func bytesToSetTrimmed(tokens *phrases.Iterator[[]byte]) map[string]struct{} {
	founds := make(map[string]struct{})
	for tokens.Next() {
		key := bytes.TrimSpace(tokens.Value())
		founds[string(key)] = exists
	}
	return founds
}

func TestPhraseBoundaries(t *testing.T) {
	t.Parallel()

	input := []byte("This should break here. And then here. 世界. I think, perhaps you can understand that — aside 🏆 🐶 here — “a quote”.")
	tokens := phrases.FromBytes(input)
	got := bytesToSetTrimmed(tokens)
	expecteds := map[string]struct{}{
		"This should break here":          exists,
		"And then here":                   exists,
		"世":                               exists, // We don't have great logic for languages without spaces. Also true for words, see Notes: https://unicode.org/reports/tr29/#WB999
		"I think":                         exists,
		"perhaps you can understand that": exists,
		"aside 🏆 🐶 here":                  exists,
		"a quote":                         exists,
	}

	for phrase := range expecteds {
		_, found := got[phrase]
		if !found {
			t.Fatalf("phrase %q was expected, not found", phrase)
		}
	}
}

func BenchmarkBytesMultilingual(b *testing.B) {
	file, err := testdata.Sample()
	if err != nil {
		b.Error(err)
	}

	b.ResetTimer()
	bytes := len(file)
	b.SetBytes(int64(bytes))

	c := 0

	for i := 0; i < b.N; i++ {
		tokens := phrases.FromBytes(file)

		for tokens.Next() {
			_ = tokens.Value()
			c++
		}
	}
}
