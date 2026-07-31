package sentences_test

import (
	"bytes"
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/sentences"
	"github.com/clipperhouse/uax29/v2/testdata"
)

func TestBytesUnicode(t *testing.T) {
	t.Parallel()

	// From the Unicode test suite; see the gen/ folder.
	var passed, failed int
	for _, test := range unicodeTests {
		test := test

		var all [][]byte
		tokens := sentences.FromBytes(test.input)
		for tokens.Next() {
			all = append(all, tokens.Value())
		}

		if !reflect.DeepEqual(all, test.expected) {
			failed++
			t.Errorf(`
	for input %v
	expected  %v
	got       %v
	spec      %s`, test.input, test.expected, all, test.comment)
		} else {
			passed++
		}
	}

	if len(unicodeTests) != passed+failed {
		t.Errorf("Incomplete %d tests: passed %d, failed %d", len(unicodeTests), passed, failed)
	}
}

func TestBytesRoundtrip(t *testing.T) {
	t.Parallel()

	const runs = 100

	tokens := sentences.FromBytes(nil)

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

	tokens := sentences.FromBytes(input)

	var output []byte
	for tokens.Next() {
		output = append(output, tokens.Value()...)
	}

	if !bytes.Equal(output, input) {
		t.Fatalf("input bytes are not the same as output bytes")
	}
}

func BenchmarkBytes(b *testing.B) {
	file, err := testdata.Sample()
	if err != nil {
		b.Error(err)
	}

	b.ResetTimer()
	b.SetBytes(int64(len(file)))

	for i := 0; i < b.N; i++ {
		tokens := sentences.FromBytes(file)
		for tokens.Next() {
			_ = tokens.Value()
		}
	}
}

func BenchmarkUnicodeTests(b *testing.B) {
	var buf bytes.Buffer
	for _, test := range unicodeTests {
		buf.Write(test.input)
	}
	file := buf.Bytes()

	b.ResetTimer()
	b.SetBytes(int64(len(file)))

	for i := 0; i < b.N; i++ {
		tokens := sentences.FromBytes(file)
		for tokens.Next() {
			_ = tokens.Value()
		}
	}
}
