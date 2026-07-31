package graphemes_test

import (
	"bytes"
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/graphemes"
	"github.com/clipperhouse/uax29/v2/testdata"
)

func TestStringUnicode(t *testing.T) {
	t.Parallel()

	// From the Unicode test suite; see the gen/ folder.
	var passed, failed int
	for _, test := range unicodeTests {
		test := test

		var all []string
		tokens := graphemes.FromString(string(test.input))
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
		tokens := graphemes.FromString(input)

		var output string
		for tokens.Next() {
			output += tokens.Value()
		}

		if output != input {
			t.Fatal("input bytes are not the same as output bytes")
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

	tokens := graphemes.FromString(string(input))

	var output string
	for tokens.Next() {
		output += tokens.Value()
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
			name:     "ASCII start",
			input:    "héllo world",
			expected: "h",
		},
		{
			name:     "combining character",
			input:    "Élvis",
			expected: "É",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "single ASCII char",
			input:    "a",
			expected: "a",
		},
		{
			name:     "pure ASCII",
			input:    "hello",
			expected: "h",
		},
		{
			name:     "emoji",
			input:    "🎉 party",
			expected: "🎉",
		},
		{
			name:     "CJK",
			input:    "日本語",
			expected: "日",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/string", func(t *testing.T) {
			g := graphemes.FromString(tt.input)
			if g.First() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, g.First())
			}
		})

		t.Run(tt.name+"/bytes", func(t *testing.T) {
			g := graphemes.FromBytes([]byte(tt.input))
			if string(g.First()) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, g.First())
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
			name:     "single printable ASCII",
			input:    "a",
			expected: "a",
		},
		{
			name:     "ASCII followed by ASCII",
			input:    "ab",
			expected: "a",
		},
		{
			name:     "ASCII space",
			input:    " hello",
			expected: " ",
		},
		{
			name:     "ASCII digit",
			input:    "5abc",
			expected: "5",
		},
		{
			name:     "ASCII punctuation",
			input:    "!hello",
			expected: "!",
		},
		// Fallback cases (non-ASCII or combining marks)
		{
			name:     "ASCII then non-ASCII",
			input:    "a日",
			expected: "a",
		},
		{
			name:     "ASCII followed by combining mark",
			input:    "e\u0301", // e + combining acute = é
			expected: "e\u0301",
		},
		{
			name:     "non-ASCII start",
			input:    "日本",
			expected: "日",
		},
		{
			name:     "emoji grapheme cluster",
			input:    "👨‍👩‍👧‍👦 family",
			expected: "👨‍👩‍👧‍👦",
		},
		{
			name:     "flag emoji",
			input:    "🇺🇸 USA",
			expected: "🇺🇸",
		},
		// Edge cases
		{
			name:     "control char (below 0x20)",
			input:    "\t hello",
			expected: "\t",
		},
		{
			name:     "DEL char (0x7F)",
			input:    "\x7Fhello",
			expected: "\x7F",
		},
		{
			name:     "high ASCII then combining",
			input:    "n\u0303", // n + combining tilde = ñ
			expected: "n\u0303",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/string", func(t *testing.T) {
			iter := graphemes.FromString(tt.input)
			got := iter.First()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})

		t.Run(tt.name+"/bytes", func(t *testing.T) {
			iter := graphemes.FromBytes([]byte(tt.input))
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
		tokens := graphemes.FromString(s)

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
		tokens := graphemes.FromString(s)

		c := 0
		for tokens.Next() {
			_ = tokens.Value()
			c++
		}

		b.ReportMetric(float64(c), "tokens")
	}
}
