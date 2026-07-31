package words_test

import (
	"bytes"
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/testdata"
	"github.com/clipperhouse/uax29/v2/words"
)

func TestStringUnicode(t *testing.T) {
	t.Parallel()

	// From the Unicode test suite; see the gen/ folder.
	var passed, failed int
	for _, test := range unicodeTests {
		test := test

		var all []string
		tokens := words.FromString(string(test.input))
		for tokens.Next() {
			all = append(all, tokens.Value())
		}

		expected := make([]string, len(test.expected))
		for i, v := range test.expected {
			expected[i] = string(v)
		}

		if !reflect.DeepEqual(all, expected) {
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

func TestStringRoundtrip(t *testing.T) {
	t.Parallel()

	const runs = 100

	for i := 0; i < runs; i++ {
		input := string(getRandomBytes())
		tokens := words.FromString(input)

		var output string
		for tokens.Next() {
			output += tokens.Value()
		}

		if output != input {
			t.Fatal("input bytes are not the same as output bytes")
		}
	}
}

func stringIterToSet(tokens *words.Iterator[string]) map[string]struct{} {
	founds := make(map[string]struct{})
	for tokens.Next() {
		founds[tokens.Value()] = struct{}{}
	}
	return founds
}

func TestStringJoiners(t *testing.T) {
	s := string(joinersInput)
	tokens1 := words.FromString(s)
	founds1 := stringIterToSet(tokens1)

	tokens2 := words.FromString(s)
	stringJoiners := &words.Joiners[string]{
		Middle:  []rune("@-/"),
		Leading: []rune("#."),
	}
	tokens2.Joiners(stringJoiners)
	founds2 := stringIterToSet(tokens2)

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

func TestStringInvalidUTF8(t *testing.T) {
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

	sc := words.FromString(string(input))

	var output string
	for sc.Next() {
		output += sc.Value()
	}

	if output != string(input) {
		t.Fatalf("input bytes are not the same as output bytes")
	}
}

func TestFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ASCII word",
			input:    "hello world",
			expected: "hello",
		},
		{
			name:     "ASCII word followed by space at end",
			input:    "hello ",
			expected: "hello",
		},
		{
			name:     "Unicode word",
			input:    "héllo world",
			expected: "héllo",
		},
		{
			name:     "CJK characters",
			input:    "日本語 text",
			expected: "日",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "single ASCII word",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "pure ASCII alphanumeric",
			input:    "abc123 def456",
			expected: "abc123",
		},
		{
			name:     "starts with space",
			input:    " hello",
			expected: " ",
		},
		{
			name:     "starts with punctuation",
			input:    "!hello",
			expected: "!",
		},
		{
			name:     "contraction",
			input:    "don't stop",
			expected: "don't",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/string", func(t *testing.T) {
			iter := words.FromString(tt.input)
			got := iter.First()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})

		t.Run(tt.name+"/bytes", func(t *testing.T) {
			iter := words.FromBytes([]byte(tt.input))
			got := string(iter.First())
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestFirstASCIIOptimization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Pure ASCII hot path cases
		{
			name:     "ASCII word followed by space then end",
			input:    "hello ",
			expected: "hello",
		},
		{
			name:     "ASCII word at end of data",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "multiple ASCII words picks first",
			input:    "hello world foo",
			expected: "hello",
		},
		// Fallback to splitFunc cases
		{
			name:     "ASCII followed by non-space punctuation",
			input:    "hello,world",
			expected: "hello",
		},
		{
			name:     "ASCII followed by non-ASCII",
			input:    "hello世界",
			expected: "hello",
		},
		{
			name:     "ASCII with mid-word apostrophe",
			input:    "don't",
			expected: "don't",
		},
		{
			name:     "ASCII with hyphen not supported by hot path",
			input:    "self-test foo",
			expected: "self",
		},
		// Single character edge cases
		{
			name:     "single ASCII letter",
			input:    "a",
			expected: "a",
		},
		{
			name:     "single ASCII digit",
			input:    "5",
			expected: "5",
		},
		{
			name:     "single space",
			input:    " ",
			expected: " ",
		},
		// Non-ASCII at start (no hot path)
		{
			name:     "starts with non-ASCII letter",
			input:    "éclair is tasty",
			expected: "éclair",
		},
		{
			name:     "starts with emoji",
			input:    "🎉 party",
			expected: "🎉",
		},
		// Numbers
		{
			name:     "pure digits",
			input:    "12345 next",
			expected: "12345",
		},
		{
			name:     "mixed alphanumeric",
			input:    "abc123def ",
			expected: "abc123def",
		},
		// Edge: ASCII then combining character
		{
			name:     "ASCII then combining mark",
			input:    "e\u0301cole", // é as e + combining acute
			expected: "e\u0301cole",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/string", func(t *testing.T) {
			iter := words.FromString(tt.input)
			got := iter.First()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})

		t.Run(tt.name+"/bytes", func(t *testing.T) {
			iter := words.FromBytes([]byte(tt.input))
			got := string(iter.First())
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func BenchmarkString(b *testing.B) {
	file, err := testdata.Sample()
	if err != nil {
		b.Error(err)
	}

	s := string(file)

	b.ResetTimer()
	b.SetBytes(int64(len(file)))

	for i := 0; i < b.N; i++ {
		tokens := words.FromString(s)
		c := 0
		for tokens.Next() {
			_ = tokens.Value()
			c++
		}

		b.ReportMetric(float64(c), "tokens")
	}
}

func BenchmarkStringUnicodeTests(b *testing.B) {
	var buf bytes.Buffer
	for _, test := range unicodeTests {
		buf.Write(test.input)
	}
	file := buf.Bytes()
	s := string(file)

	b.ResetTimer()
	b.SetBytes(int64(len(file)))

	for i := 0; i < b.N; i++ {
		tokens := words.FromString(s)

		c := 0
		for tokens.Next() {
			_ = tokens.Value()
			c++
		}

		b.ReportMetric(float64(c), "tokens")
	}
}
