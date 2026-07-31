package graphemes_test

import (
	"bytes"
	"crypto/rand"
	mathrand "math/rand"
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/graphemes"
	"github.com/clipperhouse/uax29/v2/testdata"
)

func TestScannerUnicode(t *testing.T) {
	t.Parallel()

	// From the Unicode test suite; see the gen/ folder.
	var passed, failed int
	for _, test := range unicodeTests {
		test := test

		var got [][]byte
		sc := graphemes.FromReader(bytes.NewReader(test.input))

		for sc.Scan() {
			got = append(got, sc.Bytes())
		}

		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(got, test.expected) {
			failed++
			t.Errorf(`
	for input %v
	expected  %v
	got       %v
	spec      %s`, test.input, test.expected, got, test.comment)
		} else {
			passed++
		}
	}
	t.Logf("passed %d, failed %d", passed, failed)
}

// TestScannerRoundtrip tests that all input bytes are output after iteration.
func TestScannerRoundtrip(t *testing.T) {
	t.Parallel()

	const runs = 2_000

	for i := 0; i < runs; i++ {
		input := getRandomBytes()

		r := bytes.NewReader(input)
		sc := graphemes.FromReader(r)

		var output []byte
		for sc.Scan() {
			output = append(output, sc.Bytes()...)
		}
		if err := sc.Err(); err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(output, input) {
			t.Fatal("input bytes are not the same as scanned bytes")
		}
	}
}

func TestInvalidUTF8(t *testing.T) {
	t.Parallel()

	// For background, see internal/testdata/UTF-8-test.txt, or:
	// https://www.cl.cam.ac.uk/~mgk25/ucs/examples/UTF-8-test.txt

	// Btw, don't edit UTF-8-test.txt: your editor might turn it into valid UTF-8!

	input, err := testdata.InvalidUTF8()
	inlen := len(input)

	if err != nil {
		t.Error(err)
	}

	if utf8.Valid(input) {
		t.Error("input file should not be valid utf8")
	}

	r := bytes.NewReader(input)
	sc := graphemes.FromReader(r)

	var output []byte
	for sc.Scan() {
		output = append(output, sc.Bytes()...)
	}
	if err := sc.Err(); err != nil {
		t.Error(err)
	}
	outlen := len(output)

	if inlen != outlen {
		t.Fatalf("input: %d bytes, output: %d bytes", inlen, outlen)
	}

	if !bytes.Equal(output, input) {
		t.Fatalf("input bytes are not the same as scanned bytes")
	}
}

func TestNeverZeroAtEOF(t *testing.T) {
	t.Parallel()

	// SplitFunc should never return advance = 0 when atEOF. This test is redundant
	// with the roundtrip test above, but nice to call out this invariant.

	const runs = 50
	atEOF := true

	for i := 0; i < runs; i++ {
		input := getRandomBytes()
		advance, _, _ := graphemes.SplitFunc(input, atEOF)
		if advance == 0 {
			t.Errorf("advance should never be zero (atEOF %t)", atEOF)
		}

	}
}

func TestNeverErr(t *testing.T) {
	t.Parallel()

	// SplitFunc should never return an error. This test is redundant
	// with the roundtrip test above, but nice to call out this invariant.

	const runs = 50
	atEOFs := []bool{true, false}

	for i := 0; i < runs; i++ {
		for _, atEOF := range atEOFs {
			input := getRandomBytes()

			_, _, err := graphemes.SplitFunc(input, atEOF)

			if err != nil {
				t.Errorf("SplitFunc should never error (atEOF %t)", atEOF)
			}
		}
	}
}

func getRandomBytes() []byte {
	const max = 10000
	const min = 1

	len := mathrand.Intn(max-min) + min
	b := make([]byte, len)
	_, _ = rand.Read(b)

	return b
}

func BenchmarkScanner(b *testing.B) {
	file, err := testdata.Sample()

	if err != nil {
		b.Error(err)
	}

	b.ResetTimer()
	b.SetBytes(int64(len(file)))

	r := bytes.NewReader(file)

	for i := 0; i < b.N; i++ {
		r.Reset(file)
		tokens := graphemes.FromReader(r)

		c := 0
		for tokens.Scan() {
			c++
		}
		if err := tokens.Err(); err != nil {
			b.Error(err)
		}

		b.ReportMetric(float64(c), "tokens")
	}
}

func BenchmarkUnicodeScanner(b *testing.B) {
	var buf bytes.Buffer
	for _, test := range unicodeTests {
		buf.Write(test.input)
	}
	file := buf.Bytes()

	b.ResetTimer()
	b.SetBytes(int64(len(file)))

	r := bytes.NewReader(file)

	for i := 0; i < b.N; i++ {
		r.Reset(file)
		tokens := graphemes.FromReader(r)

		c := 0
		for tokens.Scan() {
			c++
		}
		if err := tokens.Err(); err != nil {
			b.Error(err)
		}

		b.ReportMetric(float64(c), "tokens")
	}
}
