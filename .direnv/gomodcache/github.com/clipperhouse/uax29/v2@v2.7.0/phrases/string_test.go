package phrases_test

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/phrases"
	"github.com/clipperhouse/uax29/v2/testdata"
)

func TestStringRoundtrip(t *testing.T) {
	t.Parallel()

	const runs = 100

	tokens := phrases.FromString("")

	for i := 0; i < runs; i++ {
		input := string(getRandomBytes())
		tokens.SetText(input)

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

	tokens := phrases.FromBytes(input)

	var output []byte
	for tokens.Next() {
		output = append(output, tokens.Value()...)
	}

	if !bytes.Equal(output, input) {
		t.Fatalf("input bytes are not the same as output bytes")
	}
}

func stringIterToSetTrimmed(tokens *phrases.Iterator[string]) map[string]struct{} {
	founds := make(map[string]struct{})
	for tokens.Next() {
		key := strings.TrimSpace(tokens.Value())
		founds[key] = exists
	}
	return founds
}

func TestStringPhraseBoundaries(t *testing.T) {
	t.Parallel()

	input := []byte("This should break here. And then here. 世界. I think, perhaps you can understand that — aside 🏆 🐶 here — “a quote”.")
	tokens := phrases.FromString(string(input))
	got := stringIterToSetTrimmed(tokens)
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

// TestASCIIOptimization tests edge cases where the ASCII hot path in the iterator
// could potentially produce incorrect results if not implemented correctly.
func TestASCIIOptimization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		expect []string // expected phrases (exact, not trimmed)
	}{
		// All safe ASCII - should return as one phrase
		{
			name:   "all safe ASCII",
			input:  "hello world",
			expect: []string{"hello world"},
		},
		// Safe ASCII followed by breaking punctuation
		{
			name:   "comma breaks phrase",
			input:  "hello,world",
			expect: []string{"hello", ",", "world"},
		},
		// Non-safe char at start - no ASCII skip
		{
			name:   "punctuation at start",
			input:  "!hello",
			expect: []string{"!", "hello"},
		},
		// Safe ASCII followed by non-ASCII - space doesn't join with CJK
		{
			name:   "ASCII then CJK",
			input:  "hello 日本",
			expect: []string{"hello ", "日", "本"}, // CJK chars break from each other (no spaces)
		},
		// Newline is NOT in safe set, should break
		{
			name:   "newline breaks",
			input:  "hello\nworld",
			expect: []string{"hello", "\n", "world"},
		},
		// Tab is NOT in safe set (only space ' ' is safe)
		{
			name:   "tab breaks",
			input:  "hello\tworld",
			expect: []string{"hello", "\t", "world"},
		},
		// Period breaks phrases
		{
			name:   "period breaks",
			input:  "hello. world",
			expect: []string{"hello", ".", " world"},
		},
		// Numbers are safe
		{
			name:   "numbers are safe",
			input:  "abc123 def456",
			expect: []string{"abc123 def456"},
		},
		// Empty string
		{
			name:   "empty string",
			input:  "",
			expect: []string{},
		},
		// Single safe char
		{
			name:   "single safe char",
			input:  "a",
			expect: []string{"a"},
		},
		// Single non-safe char
		{
			name:   "single non-safe char",
			input:  "!",
			expect: []string{"!"},
		},
		// Trailing space
		{
			name:   "trailing space",
			input:  "hello ",
			expect: []string{"hello "},
		},
		// Leading space
		{
			name:   "leading space",
			input:  " hello",
			expect: []string{" hello"},
		},
		// Only spaces
		{
			name:   "only spaces",
			input:  "   ",
			expect: []string{"   "},
		},
		// ASCII then emoji (emoji should continue phrase in phrases package)
		{
			name:   "ASCII then emoji",
			input:  "hello 🎉",
			expect: []string{"hello 🎉"},
		},
		// Multiple punctuation - period is MidNumLet so it joins letters (WB6/WB7)
		{
			name:   "multiple punctuation",
			input:  "a.b,c!d",
			expect: []string{"a.b", ",", "c", "!", "d"},
		},
		// Apostrophe in word (contraction) - apostrophe is SingleQuote, part of MidNumLetQ
		// WB6/WB7: don't break when MidNumLetQ is between letters
		{
			name:   "apostrophe contraction don't",
			input:  "don't stop",
			expect: []string{"don't stop"},
		},
		{
			name:   "apostrophe contraction let's",
			input:  "let's go",
			expect: []string{"let's go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := phrases.FromString(tt.input)
			var got []string
			for tokens.Next() {
				got = append(got, tokens.Value())
			}

			if len(got) != len(tt.expect) {
				t.Errorf("got %d phrases %q, expected %d phrases %q", len(got), got, len(tt.expect), tt.expect)
				return
			}

			for i := range got {
				if got[i] != tt.expect[i] {
					t.Errorf("phrase %d: got %q, expected %q", i, got[i], tt.expect[i])
				}
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
		// Pure ASCII hot path - all data consumed
		{
			name:     "all ASCII alphanumeric",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "all ASCII with numbers",
			input:    "abc123 def456",
			expected: "abc123 def456",
		},
		{
			name:     "trailing space",
			input:    "hello ",
			expected: "hello ",
		},
		{
			name:     "leading space",
			input:    " hello",
			expected: " hello",
		},
		{
			name:     "only spaces",
			input:    "   ",
			expected: "   ",
		},
		// ASCII then non-ASCII triggers back-up + splitFunc
		{
			name:     "ASCII then emoji continues",
			input:    "hello 🎉 world",
			expected: "hello 🎉 world",
		},
		{
			name:     "ASCII then accented continues",
			input:    "hello café",
			expected: "hello café",
		},
		{
			name:     "ASCII then CJK breaks",
			input:    "hello 日本",
			expected: "hello ",
		},
		// Punctuation triggers back-up + splitFunc
		{
			name:     "comma breaks phrase",
			input:    "hello, world",
			expected: "hello",
		},
		{
			name:     "period breaks phrase",
			input:    "hello. world",
			expected: "hello",
		},
		{
			name:     "exclamation breaks phrase",
			input:    "hello! world",
			expected: "hello",
		},
		{
			name:     "newline breaks phrase",
			input:    "hello\nworld",
			expected: "hello",
		},
		{
			name:     "tab breaks phrase",
			input:    "hello\tworld",
			expected: "hello",
		},
		// Single character edge cases
		{
			name:     "single ASCII letter",
			input:    "a",
			expected: "a",
		},
		{
			name:     "single space",
			input:    " ",
			expected: " ",
		},
		{
			name:     "single punctuation",
			input:    "!",
			expected: "!",
		},
		// Non-ASCII at start (no hot path)
		{
			name:     "starts with non-ASCII",
			input:    "éclair is tasty",
			expected: "éclair is tasty",
		},
		{
			name:     "starts with emoji",
			input:    "🎉 party time",
			expected: "🎉 party time",
		},
		{
			name:     "starts with punctuation",
			input:    "!hello world",
			expected: "!",
		},
		// Back-up logic edge cases
		{
			name:     "one char before break",
			input:    "a,b",
			expected: "a",
		},
		{
			name:     "two chars before break",
			input:    "ab,cd",
			expected: "ab",
		},
		// Contraction handling
		{
			name:     "apostrophe in word",
			input:    "don't stop",
			expected: "don't stop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/string", func(t *testing.T) {
			iter := phrases.FromString(tt.input)
			got := iter.First()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})

		t.Run(tt.name+"/bytes", func(t *testing.T) {
			iter := phrases.FromBytes([]byte(tt.input))
			got := string(iter.First())
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
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
			name:     "ASCII phrase",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "ASCII phrase with comma",
			input:    "hello, world",
			expected: "hello",
		},
		{
			name:     "Unicode phrase",
			input:    "héllo world",
			expected: "héllo world",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "single word",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "pure ASCII alphanumeric with spaces",
			input:    "abc123 def456",
			expected: "abc123 def456",
		},
		{
			name:     "starts with punctuation",
			input:    "!hello",
			expected: "!",
		},
		{
			name:     "period breaks phrase",
			input:    "hello. world",
			expected: "hello",
		},
		{
			name:     "contraction stays together",
			input:    "don't stop",
			expected: "don't stop",
		},
		{
			name:     "ASCII then emoji",
			input:    "hello 🎉",
			expected: "hello 🎉",
		},
		{
			name:     "newline breaks phrase",
			input:    "hello\nworld",
			expected: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/string", func(t *testing.T) {
			iter := phrases.FromString(tt.input)
			got := iter.First()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})

		t.Run(tt.name+"/bytes", func(t *testing.T) {
			iter := phrases.FromBytes([]byte(tt.input))
			got := string(iter.First())
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func BenchmarkStringMultilingual(b *testing.B) {
	file, err := testdata.Sample()
	if err != nil {
		b.Error(err)
	}

	s := string(file)

	len := len(file)
	b.SetBytes(int64(len))

	b.ResetTimer()
	c := 0
	for i := 0; i < b.N; i++ {
		tokens := phrases.FromString(s)

		for tokens.Next() {
			_ = tokens.Value()
			c++
		}
	}
}

// BenchmarkStringASCII benchmarks realistic ASCII text with sentences and punctuation,
// to measure the impact of the ASCII hot path optimization.
func BenchmarkStringASCII(b *testing.B) {
	// Realistic English text with varied sentence structure and punctuation
	paragraph := `The quick brown fox jumps over the lazy dog. This sentence contains every letter of the alphabet! How fascinating is that? Well, it's been used for typing practice since the late 1800s. The phrase is simple, memorable, and effective. Teachers love it; students know it well. "Perfect for testing," they say. Numbers like 123 and 456 work too. Don't forget contractions: we'll, they're, and isn't. What about em-dashes—like this one? Or semicolons; they're useful too.`

	// Repeat to get a reasonable size
	var builder strings.Builder
	for i := 0; i < 100; i++ {
		builder.WriteString(paragraph)
		builder.WriteString(" ")
	}
	s := builder.String()

	length := len(s)
	b.SetBytes(int64(length))

	b.ResetTimer()
	c := 0

	for i := 0; i < b.N; i++ {
		tokens := phrases.FromString(s)

		for tokens.Next() {
			_ = tokens.Value()
			c++
		}
	}
}
