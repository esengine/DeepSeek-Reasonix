package words_test

import (
	"bytes"
	"reflect"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/testdata"
	"github.com/clipperhouse/uax29/v2/words"
)

func TestBytesUnicode(t *testing.T) {
	t.Parallel()

	// From the Unicode test suite; see the gen/ folder.
	var passed, failed int
	for _, test := range unicodeTests {
		test := test

		var all [][]byte
		tokens := words.FromBytes(test.input)
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

	tokens := words.FromBytes(nil)

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

func iterToSet(tokens *words.Iterator[[]byte]) map[string]struct{} {
	founds := make(map[string]struct{})
	for tokens.Next() {
		founds[string(tokens.Value())] = struct{}{}
	}
	return founds
}

func TestBytesJoiners(t *testing.T) {
	tokens1 := words.FromBytes(joinersInput)
	founds1 := iterToSet(tokens1)

	tokens2 := words.FromBytes(joinersInput)
	tokens2.Joiners(joiners)
	founds2 := iterToSet(tokens2)

	for _, test := range joinersTests {
		_, found1 := founds1[test.input]
		if found1 != test.found1 {
			t.Fatalf("For %q, expected %t for found in non-config iterator, but got %t", test.input, test.found1, found1)
		}
		_, found2 := founds2[test.input]
		if found2 != test.found2 {
			t.Fatalf("For %q, expected %t for found in iterator with joiners, but got %t", test.input, test.found2, found2)
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

	tokens := words.FromBytes(input)

	var output []byte
	for tokens.Next() {
		output = append(output, tokens.Value()...)
	}

	if !bytes.Equal(output, input) {
		t.Fatalf("input bytes are not the same as output bytes")
	}
}

func BenchmarkBytes(b *testing.B) {
	benchBytes(b, nil)
}

func benchBytes(b *testing.B, joiners *words.Joiners[[]byte]) {
	file, err := testdata.Sample()
	if err != nil {
		b.Error(err)
	}

	bytes := len(file)
	b.SetBytes(int64(bytes))

	c := 0
	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		tokens := words.FromBytes(file)
		if joiners != nil {
			tokens.Joiners(joiners)
		}

		for tokens.Next() {
			_ = tokens.Value()
			c++
		}
	}

	elapsed := time.Since(start)
	n := float64(b.N)

	tokensPerOp := float64(c) / n
	nsPerOp := float64(elapsed.Nanoseconds()) / n

	b.ReportMetric(1e3*tokensPerOp/nsPerOp, "Mtok/s")
	b.ReportMetric(tokensPerOp, "tok/op")
	b.ReportMetric(float64(bytes)/tokensPerOp, "B/tok")
}

func BenchmarkBytesJoiners(b *testing.B) {
	var joiners = &words.Joiners[[]byte]{
		Middle:  []rune("@-/"),
		Leading: []rune("#."),
	}
	benchBytes(b, joiners)
}

func BenchmarkBytesUnicodeTests(b *testing.B) {
	var buf bytes.Buffer
	for _, test := range unicodeTests {
		buf.Write(test.input)
	}
	file := buf.Bytes()

	b.ResetTimer()
	b.SetBytes(int64(len(file)))

	for i := 0; i < b.N; i++ {
		tokens := words.FromBytes(file)

		c := 0
		for tokens.Next() {
			_ = tokens.Value()
			c++
		}

		b.ReportMetric(float64(c), "tokens")
	}
}
