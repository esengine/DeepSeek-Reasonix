package words_test

import (
	"bytes"
	"crypto/rand"
	mathrand "math/rand"
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/testdata"
	"github.com/clipperhouse/uax29/v2/words"
)

func TestScannerUnicode(t *testing.T) {
	t.Parallel()

	// From the Unicode test suite; see the gen/ folder.
	var passed, failed int
	for _, test := range unicodeTests {
		var scanned [][]byte
		scanner := words.FromReader(bytes.NewReader(test.input))
		for scanner.Scan() {
			scanned = append(scanned, scanner.Bytes())
		}

		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(scanned, test.expected) {
			failed++
			t.Errorf(`
	for input %v
	expected  %v
	got       %v
	spec      %s`, test.input, test.expected, scanned, test.comment)
		} else {
			passed++
		}
	}
	t.Logf("%d tests: passed %d, failed %d", len(unicodeTests), passed, failed)
}

func TestScannerRoundtrip(t *testing.T) {
	t.Parallel()

	const runs = 100

	for i := 0; i < runs; i++ {

		input := getRandomBytes()

		r := bytes.NewReader(input)
		sc := words.FromReader(r)

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

func scanToSet(tokens *words.Scanner) map[string]struct{} {
	founds := make(map[string]struct{})
	for tokens.Scan() {
		founds[string(tokens.Bytes())] = struct{}{}
	}
	return founds
}

func TestScannerJoiners(t *testing.T) {
	r1 := bytes.NewReader(joinersInput)
	tokens1 := words.FromReader(r1)
	founds1 := scanToSet(tokens1)

	r2 := bytes.NewReader(joinersInput)
	tokens2 := words.FromReader(r2)
	tokens2.Joiners(joiners)
	founds2 := scanToSet(tokens2)

	for _, test := range joinersTests {
		_, found1 := founds1[test.input]
		if found1 != test.found1 {
			t.Fatalf("For %q, expected %t for found in non-config scanner, but got %t", test.input, test.found1, found1)
		}
		_, found2 := founds2[test.input]
		if found2 != test.found2 {
			t.Fatalf("For %q, expected %t for found in scanner with joiners, but got %t", test.input, test.found2, found2)
		}
	}
}

func TestInvalidUTF8(t *testing.T) {
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

	r := bytes.NewReader(input)
	tokens := words.FromReader(r)

	var output []byte
	for tokens.Scan() {
		output = append(output, tokens.Bytes()...)
	}

	if err := tokens.Err(); err != nil {
		t.Error(err)
	}

	if !bytes.Equal(output, input) {
		t.Fatalf("input bytes are not the same as scanned bytes")
	}
}

func TestNeverZeroAtEOF(t *testing.T) {
	t.Parallel()

	// SplitFunc should never return advance = 0 when atEOF. This test is redundant
	// with the roundtrip test above, but nice to call out this invariant.

	const runs = 100
	atEOF := true

	for i := 0; i < runs; i++ {
		input := getRandomBytes()
		advance, _, _ := words.SplitFunc(input, atEOF)
		if advance == 0 {
			t.Errorf("advance should never be zero (atEOF %t)", atEOF)
		}
	}
}

func TestNeverErr(t *testing.T) {
	t.Parallel()

	// SplitFunc should never return an error. This test is redundant
	// with the roundtrip test above, but nice to call out this invariant.

	const runs = 100
	atEOFs := []bool{true, false}

	for i := 0; i < runs; i++ {
		for _, atEOF := range atEOFs {
			input := getRandomBytes()

			_, _, err := words.SplitFunc(input, atEOF)

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
		sc := words.FromReader(r)

		c := 0
		for sc.Scan() {
			c++
		}
		if err := sc.Err(); err != nil {
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
		sc := words.FromReader(r)

		c := 0
		for sc.Scan() {
			c++
		}
		if err := sc.Err(); err != nil {
			b.Error(err)
		}

		b.ReportMetric(float64(c), "tokens")
	}
}
