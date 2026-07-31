package sentences_test

import (
	"bytes"
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/sentences"
	"github.com/clipperhouse/uax29/v2/testdata"
)

func TestStringUnicode(t *testing.T) {
	t.Parallel()

	// From the Unicode test suite; see the gen/ folder.
	var passed, failed int
	for _, test := range unicodeTests {
		test := test

		var all []string
		tokens := sentences.FromString(string(test.input))
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
		tokens := sentences.FromString(input)

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

	tokens := sentences.FromString(string(input))

	var output string
	for tokens.Next() {
		output += tokens.Value()
	}

	if output != string(input) {
		t.Fatalf("input bytes are not the same as output bytes")
	}
}

// TestASCIIOptimization tests edge cases for the ASCII hot path optimization
// in the iterator. The optimization fast-forwards over [a-zA-Z0-9 ] and defers
// to splitfunc for everything else.
func TestASCIIOptimization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		// Basic sentence breaks after ASCII text
		{
			name:     "period after ASCII",
			input:    "Hello world.",
			expected: []string{"Hello world."},
		},
		{
			name:     "exclamation after ASCII",
			input:    "Hello world!",
			expected: []string{"Hello world!"},
		},
		{
			name:     "question after ASCII",
			input:    "Hello world?",
			expected: []string{"Hello world?"},
		},

		// Multiple sentences
		{
			name:     "two sentences with space",
			input:    "Hello. World.",
			expected: []string{"Hello. ", "World."},
		},
		{
			name:     "no space after period stays together",
			input:    "Hello.World.",
			expected: []string{"Hello.World."}, // SB7: Lower ATerm × Upper = no break
		},
		{
			name:     "three sentences",
			input:    "One. Two. Three.",
			expected: []string{"One. ", "Two. ", "Three."},
		},

		// Numbers and decimals (SB6: ATerm × Numeric)
		{
			name:     "decimal number",
			input:    "The value is 3.14 exactly.",
			expected: []string{"The value is 3.14 exactly."},
		},
		{
			name:     "price",
			input:    "It costs 9.99 dollars.",
			expected: []string{"It costs 9.99 dollars."},
		},
		{
			name:     "version number",
			input:    "Use version 2.0 now.",
			expected: []string{"Use version 2.0 now."},
		},

		// Abbreviations and SB7 (Lower ATerm × Upper = no break when no space)
		{
			name:     "abbreviation with space breaks",
			input:    "Dr. Smith is here.",
			expected: []string{"Dr. ", "Smith is here."}, // SB11: space after ATerm causes break
		},
		{
			name:     "abbreviation no space stays together",
			input:    "Dr.Smith is here.",
			expected: []string{"Dr.Smith is here."}, // SB7: Lower ATerm × Upper = no break
		},
		{
			name:     "abbreviation U.S.A followed by lowercase",
			input:    "Visit the U.S.A. today.",
			expected: []string{"Visit the U.S.A. today."}, // SB8: ATerm Sp × Lower = no break
		},
		{
			name:     "abbreviation U.S.A followed by uppercase",
			input:    "Visit the U.S.A. Today.",
			expected: []string{"Visit the U.S.A. ", "Today."}, // SB11: Sp after ATerm, then Upper = break
		},

		// Newlines and paragraph separators
		{
			name:     "newline after ASCII",
			input:    "Hello\nWorld",
			expected: []string{"Hello\n", "World"},
		},
		{
			name:     "CRLF after ASCII",
			input:    "Hello\r\nWorld",
			expected: []string{"Hello\r\n", "World"},
		},

		// Pure ASCII with no breaks
		{
			name:     "all ASCII alphanumeric",
			input:    "The quick brown fox jumps over 42 lazy dogs",
			expected: []string{"The quick brown fox jumps over 42 lazy dogs"},
		},
		{
			name:     "all ASCII letters",
			input:    "abcdefghijklmnopqrstuvwxyz",
			expected: []string{"abcdefghijklmnopqrstuvwxyz"},
		},

		// Edge: single char before terminator (tests back-up logic)
		{
			name:     "single char sentences",
			input:    "A. B. C.",
			expected: []string{"A. ", "B. ", "C."},
		},

		// Edge: terminator immediately after fast-forward position
		{
			name:     "long ASCII then period",
			input:    "This is a very long sentence with many words. Next.",
			expected: []string{"This is a very long sentence with many words. ", "Next."},
		},

		// Edge: mixed numbers and sentence breaks
		{
			name:     "number then sentence break",
			input:    "Buy 3. Get 1 free.",
			expected: []string{"Buy 3. ", "Get 1 free."},
		},

		// Edge: ASCII followed by non-ASCII
		{
			name:     "ASCII then emoji",
			input:    "Hello 😀 world.",
			expected: []string{"Hello 😀 world."},
		},
		{
			name:     "ASCII then accented",
			input:    "Hello café world.",
			expected: []string{"Hello café world."},
		},

		// Edge: spaces around terminators
		{
			name:     "multiple spaces after period",
			input:    "Hello.  World.",
			expected: []string{"Hello.  ", "World."},
		},

		// Edge: empty and minimal
		{
			name:     "single word",
			input:    "Hello",
			expected: []string{"Hello"},
		},
		{
			name:     "single char",
			input:    "A",
			expected: []string{"A"},
		},
		{
			name:     "just spaces",
			input:    "   ",
			expected: []string{"   "},
		},

		// Edge: alternating ASCII and non-ASCII
		{
			name:     "alternating",
			input:    "A中B中C.",
			expected: []string{"A中B中C."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			iter := sentences.FromString(tt.input)
			for iter.Next() {
				got = append(got, iter.Value())
			}

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("input %q:\n  expected: %q\n  got:      %q", tt.input, tt.expected, got)
			}

			// Also verify roundtrip
			var combined string
			for _, s := range got {
				combined += s
			}
			if combined != tt.input {
				t.Errorf("roundtrip failed: input %q, combined %q", tt.input, combined)
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
			name:     "all ASCII alphanumeric no terminator",
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
			name:     "only spaces",
			input:    "   ",
			expected: "   ",
		},
		// ASCII then terminator triggers back-up + splitFunc
		{
			name:     "period ends sentence",
			input:    "Hello world.",
			expected: "Hello world.",
		},
		{
			name:     "period then more text",
			input:    "Hello. World.",
			expected: "Hello. ",
		},
		{
			name:     "exclamation ends sentence",
			input:    "Hello!",
			expected: "Hello!",
		},
		{
			name:     "question ends sentence",
			input:    "Hello?",
			expected: "Hello?",
		},
		{
			name:     "newline breaks sentence",
			input:    "Hello\nWorld",
			expected: "Hello\n",
		},
		// ASCII then non-ASCII triggers back-up + splitFunc
		{
			name:     "ASCII then emoji continues",
			input:    "Hello 😀 world.",
			expected: "Hello 😀 world.",
		},
		{
			name:     "ASCII then accented continues",
			input:    "Hello café.",
			expected: "Hello café.",
		},
		// Single character edge cases
		{
			name:     "single ASCII letter",
			input:    "A",
			expected: "A",
		},
		{
			name:     "single space",
			input:    " ",
			expected: " ",
		},
		{
			name:     "single period",
			input:    ".",
			expected: ".",
		},
		// Non-ASCII at start (no hot path)
		{
			name:     "starts with non-ASCII",
			input:    "Éclair is tasty.",
			expected: "Éclair is tasty.",
		},
		{
			name:     "starts with emoji",
			input:    "🎉 Party time!",
			expected: "🎉 Party time!",
		},
		// Back-up logic edge cases
		{
			name:     "one char before terminator",
			input:    "A. B.",
			expected: "A. ",
		},
		{
			name:     "two chars before terminator",
			input:    "AB. CD.",
			expected: "AB. ",
		},
		// Decimal numbers (SB6: ATerm × Numeric)
		{
			name:     "decimal stays together",
			input:    "Pi is 3.14 exactly.",
			expected: "Pi is 3.14 exactly.",
		},
		{
			name:     "version number",
			input:    "Use v2.0 now.",
			expected: "Use v2.0 now.",
		},
		// Abbreviations
		{
			name:     "abbreviation Dr. with space breaks",
			input:    "Dr. Smith arrived.",
			expected: "Dr. ",
		},
		{
			name:     "abbreviation no space stays",
			input:    "Dr.Smith arrived.",
			expected: "Dr.Smith arrived.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/string", func(t *testing.T) {
			iter := sentences.FromString(tt.input)
			got := iter.First()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})

		t.Run(tt.name+"/bytes", func(t *testing.T) {
			iter := sentences.FromBytes([]byte(tt.input))
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
			name:     "single sentence",
			input:    "Hello world.",
			expected: "Hello world.",
		},
		{
			name:     "two sentences",
			input:    "Hello. World.",
			expected: "Hello. ",
		},
		{
			name:     "ASCII no terminator",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "Unicode sentence",
			input:    "Héllo world.",
			expected: "Héllo world.",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "pure ASCII alphanumeric",
			input:    "abc123 def456",
			expected: "abc123 def456",
		},
		{
			name:     "newline breaks sentence",
			input:    "Hello\nWorld",
			expected: "Hello\n",
		},
		{
			name:     "exclamation ends sentence",
			input:    "Hello! World.",
			expected: "Hello! ",
		},
		{
			name:     "question ends sentence",
			input:    "Hello? World.",
			expected: "Hello? ",
		},
		{
			name:     "decimal number stays together",
			input:    "The value is 3.14 exactly.",
			expected: "The value is 3.14 exactly.",
		},
		{
			name:     "abbreviation with space breaks",
			input:    "Dr. Smith is here.",
			expected: "Dr. ",
		},
		{
			name:     "ASCII then emoji",
			input:    "Hello 😀 world.",
			expected: "Hello 😀 world.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/string", func(t *testing.T) {
			iter := sentences.FromString(tt.input)
			got := iter.First()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})

		t.Run(tt.name+"/bytes", func(t *testing.T) {
			iter := sentences.FromBytes([]byte(tt.input))
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

	b.ResetTimer()
	b.SetBytes(int64(len(file)))

	for i := 0; i < b.N; i++ {
		tokens := sentences.FromString(s)

		for tokens.Next() {
			_ = tokens.Value()
		}
	}
}

// asciiText is realistic English prose for benchmarking the ASCII optimization.
// It contains a mix of sentence lengths, punctuation, numbers, and abbreviations.
const asciiText = `The quick brown fox jumps over the lazy dog. This sentence contains every letter of the alphabet. How fascinating is that? Very fascinating indeed!

Dr. Smith arrived at 9:30 a.m. to discuss the quarterly results. The company earned $3.14 million in Q4. Revenue increased by 12.5 percent year over year. These numbers exceeded all expectations.

Programming languages have evolved significantly since the 1950s. FORTRAN was one of the first high-level languages. Today we have Python, Go, Rust, and many others. Each language has its strengths and weaknesses. Which one is best? That depends on your use case.

The United States of America declared independence in 1776. The Constitution was ratified in 1788. George Washington became the first president in 1789. These events shaped the modern world.

Email addresses like user@example.com are common in text. URLs such as https://www.example.org appear frequently too. Phone numbers like 555-123-4567 need special handling. Version numbers like v2.0.1 should stay together.

Short sentences work. Long sentences with many clauses and subclauses that go on and on and contain lots of information can be harder to parse but are still valid English prose that appears in academic writing and legal documents. Medium length sentences offer a good balance between clarity and detail.

The meeting is scheduled for Monday at 2 p.m. in Conference Room B. Please bring your laptop and any relevant documents. We will discuss the roadmap for Q1 2024. Attendance is mandatory for all team leads.

In conclusion, this benchmark text provides realistic English prose with varied sentence structures, punctuation marks, numbers, abbreviations, and formatting. It should effectively test the ASCII optimization path in the sentence segmentation algorithm.`

func BenchmarkStringASCII(b *testing.B) {
	// Repeat the text to get a substantial size
	var buf bytes.Buffer
	for i := 0; i < 100; i++ {
		buf.WriteString(asciiText)
		buf.WriteString("\n\n")
	}
	s := buf.String()

	b.ResetTimer()
	b.SetBytes(int64(len(s)))

	for i := 0; i < b.N; i++ {
		tokens := sentences.FromString(s)

		for tokens.Next() {
			_ = tokens.Value()
		}
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
		tokens := sentences.FromString(s)

		for tokens.Next() {
			_ = tokens.Value()
		}
	}
}
