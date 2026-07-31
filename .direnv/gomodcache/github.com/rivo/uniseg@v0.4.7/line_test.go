package uniseg

import "testing"

// Test all official Unicode test cases for line breaks using the byte slice
// function.
func TestLineCasesBytes(t *testing.T) {
	for testNum, testCase := range lineBreakTestCases {
		/*t.Logf(`Test case %d %q: Expecting %x, getting %x, code points %x"`,
		testNum,
		strings.TrimSpace(testCase.original),
		testCase.expected,
		decomposed(testCase.original),
		[]rune(testCase.original))*/
		var (
			segment []byte
			index   int
		)
		state := -1
		b := []byte(testCase.original)
	WordLoop:
		for index = 0; len(b) > 0; index++ {
			if index >= len(testCase.expected) {
				t.Errorf(`Test case %d %q failed: More segments %d returned than expected %d`,
					testNum,
					testCase.original,
					index,
					len(testCase.expected))
				break
			}
			segment, b, _, state = FirstLineSegment(b, state)
			cluster := []rune(string(segment))
			if len(cluster) != len(testCase.expected[index]) {
				t.Errorf(`Test case %d %q failed: Segment at index %d has %d codepoints %x, %d expected %x`,
					testNum,
					testCase.original,
					index,
					len(cluster),
					cluster,
					len(testCase.expected[index]),
					testCase.expected[index])
				break
			}
			for i, r := range cluster {
				if r != testCase.expected[index][i] {
					t.Errorf(`Test case %d %q failed: Segment at index %d is %x, expected %x`,
						testNum,
						testCase.original,
						index,
						cluster,
						testCase.expected[index])
					break WordLoop
				}
			}
		}
		if index < len(testCase.expected) {
			t.Errorf(`Test case %d %q failed: Fewer segments returned (%d) than expected (%d)`,
				testNum,
				testCase.original,
				index,
				len(testCase.expected))
		}
	}
	segment, rest, mustBreak, newState := FirstLineSegment([]byte{}, -1)
	if len(segment) > 0 {
		t.Errorf(`Expected segment to be empty byte slice, got %q`, segment)
	}
	if len(rest) > 0 {
		t.Errorf(`Expected rest to be empty byte slice, got %q`, rest)
	}
	if mustBreak {
		t.Error(`Expected mustBreak to be false, got true`)
	}
	if newState != 0 {
		t.Errorf(`Expected newState to be 0, got %d`, newState)
	}
}

// Test all official Unicode test cases for line breaks using the string
// function.
func TestLineCasesString(t *testing.T) {
	for testNum, testCase := range lineBreakTestCases {
		/*t.Logf(`Test case %d %q: Expecting %x, getting %x, code points %x"`,
		testNum,
		strings.TrimSpace(testCase.original),
		testCase.expected,
		decomposed(testCase.original),
		[]rune(testCase.original))*/
		var (
			segment string
			index   int
		)
		state := -1
		str := testCase.original
	WordLoop:
		for index = 0; len(str) > 0; index++ {
			if index >= len(testCase.expected) {
				t.Errorf(`Test case %d %q failed: More segments %d returned than expected %d`,
					testNum,
					testCase.original,
					index,
					len(testCase.expected))
				break
			}
			segment, str, _, state = FirstLineSegmentInString(str, state)
			cluster := []rune(string(segment))
			if len(cluster) != len(testCase.expected[index]) {
				t.Errorf(`Test case %d %q failed: Segment at index %d has %d codepoints %x, %d expected %x`,
					testNum,
					testCase.original,
					index,
					len(cluster),
					cluster,
					len(testCase.expected[index]),
					testCase.expected[index])
				break
			}
			for i, r := range cluster {
				if r != testCase.expected[index][i] {
					t.Errorf(`Test case %d %q failed: Segment at index %d is %x, expected %x`,
						testNum,
						testCase.original,
						index,
						cluster,
						testCase.expected[index])
					break WordLoop
				}
			}
		}
		if index < len(testCase.expected) {
			t.Errorf(`Test case %d %q failed: Fewer segments returned (%d) than expected (%d)`,
				testNum,
				testCase.original,
				index,
				len(testCase.expected))
		}
	}
	segment, rest, mustBreak, newState := FirstLineSegmentInString("", -1)
	if len(segment) > 0 {
		t.Errorf(`Expected segment to be empty string, got %q`, segment)
	}
	if len(rest) > 0 {
		t.Errorf(`Expected rest to be empty string, got %q`, rest)
	}
	if mustBreak {
		t.Error(`Expected mustBreak to be false, got true`)
	}
	if newState != 0 {
		t.Errorf(`Expected newState to be 0, got %d`, newState)
	}
}

var hasTrailingLineBreakTestCases = []struct {
	input string
	want  bool
}{
	{"\v", true},     // prBK
	{"\r", true},     // prCR
	{"\n", true},     // prLF
	{"\u0085", true}, // prNL
	{" ", false},
	{"A", false},
	{"", false},
}

func TestHasTrailingLineBreak(t *testing.T) {
	for _, tt := range hasTrailingLineBreakTestCases {
		got := HasTrailingLineBreak([]byte(tt.input))
		if got != tt.want {
			t.Errorf("HasTrailingLineBreak(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestHasTrailingLineBreakInString(t *testing.T) {
	for _, tt := range hasTrailingLineBreakTestCases {
		got := HasTrailingLineBreakInString(tt.input)
		if got != tt.want {
			t.Errorf("HasTrailingLineBreak(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// Benchmark the use of the line break function for byte slices.
func BenchmarkLineFunctionBytes(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var c []byte
		state := -1
		str := benchmarkBytes
		for len(str) > 0 {
			c, str, _, state = FirstLineSegment(str, state)
			resultRunes = []rune(string(c))
		}
	}
}

// Benchmark the use of the line break function for strings.
func BenchmarkLineFunctionString(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var c string
		state := -1
		str := benchmarkStr
		for len(str) > 0 {
			c, str, _, state = FirstLineSegmentInString(str, state)
			resultRunes = []rune(c)
		}
	}
}
