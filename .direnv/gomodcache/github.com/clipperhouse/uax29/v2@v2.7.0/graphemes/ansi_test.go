package graphemes_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/clipperhouse/uax29/v2/graphemes"
	"github.com/clipperhouse/uax29/v2/testdata"
)

type ansiCase struct {
	name     string
	input    string
	expected []string
}

func assertANSITokens(t *testing.T, input string, expected []string, sevenBit, eightBit bool) {
	t.Helper()

	assertEqual := func(kind string, got []string) {
		t.Helper()
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("%s mismatch\ngot %q\nexpected %q", kind, got, expected)
		}
	}

	iterString := graphemes.FromString(input)
	iterString.AnsiEscapeSequences = sevenBit
	iterString.AnsiEscapeSequences8Bit = eightBit
	var gotString []string
	for iterString.Next() {
		gotString = append(gotString, iterString.Value())
	}
	assertEqual("string", gotString)

	iterBytes := graphemes.FromBytes([]byte(input))
	iterBytes.AnsiEscapeSequences = sevenBit
	iterBytes.AnsiEscapeSequences8Bit = eightBit
	var gotBytes []string
	for iterBytes.Next() {
		gotBytes = append(gotBytes, string(iterBytes.Value()))
	}
	assertEqual("bytes", gotBytes)
}

func runANSICases(t *testing.T, tests []ansiCase, sevenBit, eightBit bool) {
	t.Helper()
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertANSITokens(t, tt.input, tt.expected, sevenBit, eightBit)
		})
	}
}

func TestAnsiEscapeSequences7BitOnlyAsGraphemes(t *testing.T) {
	t.Parallel()

	tests := []ansiCase{
		{name: "SGR reset", input: "\x1b[0m", expected: []string{"\x1b[0m"}},
		{name: "SGR red then text", input: "\x1b[31mhello", expected: []string{"\x1b[31m", "h", "e", "l", "l", "o"}},
		{name: "CSI with valid intermediate", input: "\x1b[0 q", expected: []string{"\x1b[0 q"}},
		{name: "OSC window title then BEL", input: "\x1b]0;My Title\x07", expected: []string{"\x1b]0;My Title\x07"}},
		{name: "OSC window title then ST", input: "\x1b]0;Title\x1b\\", expected: []string{"\x1b]0;Title\x1b\\"}},
		{name: "DCS with ST terminator", input: "\x1bPq#0;2;0;0;0\x1b\\", expected: []string{"\x1bPq#0;2;0;0;0\x1b\\"}},
		{name: "DCS canceled by CAN", input: "\x1bPqdata\x18z", expected: []string{"\x1bPqdata", "\x18", "z"}},
		{name: "SOS with ST terminator", input: "\x1bXhello\x1b\\", expected: []string{"\x1bXhello\x1b\\"}},
		{name: "PM with ST terminator", input: "\x1b^msg\x1b\\", expected: []string{"\x1b^msg\x1b\\"}},
		{name: "APC with ST terminator", input: "\x1b_data\x1b\\", expected: []string{"\x1b_data\x1b\\"}},
		{name: "two-byte Fe", input: "\x1bD", expected: []string{"\x1bD"}},
		{name: "two-byte Fp", input: "\x1b7", expected: []string{"\x1b7"}},
		{name: "nF with multiple intermediates", input: "\x1b !Fx", expected: []string{"\x1b !F", "x"}},
		{name: "malformed CSI remains split", input: "\x1b[ 1mok", expected: []string{"\x1b", "[", " ", "1", "m", "o", "k"}},
		{name: "C1 CSI is not parsed", input: "\x9B31mhello", expected: []string{"\x9B", "3", "1", "m", "h", "e", "l", "l", "o"}},
		{name: "7-bit OSC does not accept C1 ST", input: "\x1b]0;Title\x9Cz", expected: []string{"\x1b", "]", "0", ";", "T", "i", "t", "l", "e", "\x9C", "z"}},
	}

	runANSICases(t, tests, true, false)
}

func TestAnsiEscapeSequences8BitOnlyAsGraphemes(t *testing.T) {
	t.Parallel()

	tests := []ansiCase{
		{name: "C1 CSI then text", input: "\x9B31mhello", expected: []string{"\x9B31m", "h", "e", "l", "l", "o"}},
		{name: "C1 CSI multiple params", input: "\x9B1;2;3m", expected: []string{"\x9B1;2;3m"}},
		{name: "C1 OSC with C1 ST", input: "\x9D0;Title\x9C", expected: []string{"\x9D0;Title\x9C"}},
		{name: "C1 OSC with 7-bit ST is not parsed as one sequence", input: "\x9D0;Title\x1b\\", expected: []string{"\x9D", "0", ";", "T", "i", "t", "l", "e", "\x1b", "\\"}},
		{name: "C1 DCS with C1 ST", input: "\x90qpayload\x9C", expected: []string{"\x90qpayload\x9C"}},
		{name: "C1 DCS with 7-bit ST is not parsed as one sequence", input: "\x90qpayload\x1b\\", expected: []string{"\x90", "q", "p", "a", "y", "l", "o", "a", "d", "\x1b", "\\"}},
		{name: "C1 DCS canceled by CAN", input: "\x90qpayload\x18x", expected: []string{"\x90qpayload", "\x18", "x"}},
		{name: "C1 SOS with C1 ST", input: "\x98hello\x9C", expected: []string{"\x98hello\x9C"}},
		{name: "C1 PM with 7-bit ST is not parsed as one sequence", input: "\x9Emsg\x1b\\", expected: []string{"\x9E", "m", "s", "g", "\x1b", "\\"}},
		{name: "C1 APC with C1 ST", input: "\x9Fdata\x9C", expected: []string{"\x9Fdata\x9C"}},
		{name: "single C1 Fe control", input: "\x84", expected: []string{"\x84"}},
		{name: "C1 OSC unterminated", input: "\x9D0;title", expected: []string{"\x9D", "0", ";", "t", "i", "t", "l", "e"}},
		{name: "C1 DCS unterminated", input: "\x90data", expected: []string{"\x90", "d", "a", "t", "a"}},
		{name: "7-bit ESC sequence is not parsed", input: "\x1b[31mhello", expected: []string{"\x1b", "[", "3", "1", "m", "h", "e", "l", "l", "o"}},
	}

	runANSICases(t, tests, false, true)
}

func TestAnsiEscapeSequencesBothEnabledAsGraphemes(t *testing.T) {
	t.Parallel()

	tests := []ansiCase{
		{
			name:     "SGR reset",
			input:    "\x1b[0m",
			expected: []string{"\x1b[0m"},
		},
		{
			name:     "SGR red then text",
			input:    "\x1b[31mhello",
			expected: []string{"\x1b[31m", "h", "e", "l", "l", "o"},
		},
		{
			name:     "CSI with params and final",
			input:    "\x1b[1;32m",
			expected: []string{"\x1b[1;32m"},
		},
		{
			name:     "OSC window title then BEL",
			input:    "\x1b]0;My Title\x07",
			expected: []string{"\x1b]0;My Title\x07"},
		},
		{
			name:     "OSC UTF-8 payload does not terminate at continuation byte",
			input:    "\x1b]0;本\x07",
			expected: []string{"\x1b]0;本\x07"},
		},
		{
			name:     "OSC window title then ST",
			input:    "\x1b]0;Title\x1b\\",
			expected: []string{"\x1b]0;Title\x1b\\"},
		},
		{
			name:     "DCS with ST terminator",
			input:    "\x1bPq#0;2;0;0;0\x1b\\",
			expected: []string{"\x1bPq#0;2;0;0;0\x1b\\"},
		},
		{
			name:     "DCS UTF-8 payload does not terminate at continuation byte",
			input:    "\x1bPq本\x1b\\",
			expected: []string{"\x1bPq本\x1b\\"},
		},
		{
			name:     "DCS with BEL in payload is not a single sequence",
			input:    "\x1bPq\x07rest",
			expected: []string{"\x1b", "P", "q", "\x07", "r", "e", "s", "t"},
		},
		{
			name:     "DCS canceled by CAN",
			input:    "\x1bPqdata\x18z",
			expected: []string{"\x1bPqdata", "\x18", "z"},
		},
		{
			name:     "DCS canceled immediately by CAN",
			input:    "\x1bP\x18z",
			expected: []string{"\x1bP", "\x18", "z"},
		},
		{
			name:     "DCS canceled by SUB",
			input:    "\x1bPqdata\x1az",
			expected: []string{"\x1bPqdata", "\x1a", "z"},
		},
		{
			name:     "SOS with ST terminator",
			input:    "\x1bXhello\x1b\\",
			expected: []string{"\x1bXhello\x1b\\"},
		},
		{
			name:     "SOS with BEL in payload is not a single sequence",
			input:    "\x1bXhi\x07",
			expected: []string{"\x1b", "X", "h", "i", "\x07"},
		},
		{
			name:     "PM with ST terminator",
			input:    "\x1b^msg\x1b\\",
			expected: []string{"\x1b^msg\x1b\\"},
		},
		{
			name:     "PM with BEL in payload is not a single sequence",
			input:    "\x1b^m\x07",
			expected: []string{"\x1b", "^", "m", "\x07"},
		},
		{
			name:     "APC with ST terminator",
			input:    "\x1b_data\x1b\\",
			expected: []string{"\x1b_data\x1b\\"},
		},
		{
			name:     "APC with BEL in payload is not a single sequence",
			input:    "\x1b_d\x07",
			expected: []string{"\x1b", "_", "d", "\x07"},
		},
		{
			name:     "OSC empty payload with BEL",
			input:    "\x1b]\x07",
			expected: []string{"\x1b]\x07"},
		},
		{
			name:     "DCS unterminated",
			input:    "\x1bPdata",
			expected: []string{"\x1b", "P", "d", "a", "t", "a"},
		},
		{
			name:     "OSC unterminated",
			input:    "\x1b]0;title",
			expected: []string{"\x1b", "]", "0", ";", "t", "i", "t", "l", "e"},
		},
		{
			name:     "OSC canceled by CAN",
			input:    "\x1b]0;title\x18x",
			expected: []string{"\x1b]0;title", "\x18", "x"},
		},
		{
			name:     "OSC canceled by SUB",
			input:    "\x1b]0;title\x1ax",
			expected: []string{"\x1b]0;title", "\x1a", "x"},
		},
		{
			name:     "two-byte Fe",
			input:    "\x1bD", // IND
			expected: []string{"\x1bD"},
		},
		{
			name:     "two-byte Fs RIS",
			input:    "\x1bc",
			expected: []string{"\x1bc"},
		},
		{
			name:     "two-byte Fs upper boundary 0x7E",
			input:    "\x1b~x",
			expected: []string{"\x1b~", "x"},
		},
		{
			name:     "ESC DEL (0x7F) is not Fs",
			input:    "\x1b\x7f",
			expected: []string{"\x1b", "\x7f"},
		},
		{
			name:     "two-byte Fp DECSC",
			input:    "\x1b7",
			expected: []string{"\x1b7"},
		},
		{
			name:     "nF ESC SP F",
			input:    "\x1b F",
			expected: []string{"\x1b F"},
		},
		{
			name:     "mixed: CSI then letter",
			input:    "\x1b[mx",
			expected: []string{"\x1b[m", "x"},
		},
		{
			name:     "C1 CSI then text",
			input:    "\x9B31mhello",
			expected: []string{"\x9B31m", "h", "e", "l", "l", "o"},
		},
		{
			name:     "C1 OSC with C1 ST terminator",
			input:    "\x9D0;Title\x9C",
			expected: []string{"\x9D0;Title\x9C"},
		},
		{
			name:     "C1 OSC with 7-bit ST terminator",
			input:    "\x9D0;Title\x1b\\",
			expected: []string{"\x9D", "0", ";", "T", "i", "t", "l", "e", "\x1b\\"},
		},
		{
			name:     "7-bit OSC with C1 ST terminator",
			input:    "\x1b]0;Title\x9C",
			expected: []string{"\x1b", "]", "0", ";", "T", "i", "t", "l", "e", "\x9C"},
		},
		{
			name:     "C1 DCS with C1 ST terminator",
			input:    "\x90qpayload\x9C",
			expected: []string{"\x90qpayload\x9C"},
		},
		{
			name:     "C1 DCS canceled by CAN",
			input:    "\x90qpayload\x18x",
			expected: []string{"\x90qpayload", "\x18", "x"},
		},
		{
			name:     "C1 DCS with 7-bit ST terminator",
			input:    "\x90qpayload\x1b\\",
			expected: []string{"\x90", "q", "p", "a", "y", "l", "o", "a", "d", "\x1b\\"},
		},
		{
			name:     "7-bit DCS with C1 ST terminator",
			input:    "\x1bPqpayload\x9C",
			expected: []string{"\x1b", "P", "q", "p", "a", "y", "l", "o", "a", "d", "\x9C"},
		},
		{
			name:     "C1 Fe IND control",
			input:    "\x84",
			expected: []string{"\x84"},
		},
		{
			name:     "nF malformed: no final byte",
			input:    "\x1b \x1b",
			expected: []string{"\x1b", " ", "\x1b"},
		},
		{
			name:     "nF with multiple intermediates",
			input:    "\x1b !Fx",
			expected: []string{"\x1b !F", "x"},
		},
		{
			name:     "nF with private-use final (0x30)",
			input:    "\x1b 0",
			expected: []string{"\x1b 0"},
		},
		{
			name:     "CSI with valid intermediate byte",
			input:    "\x1b[0 q",
			expected: []string{"\x1b[0 q"},
		},
		{
			name:     "C1 OSC unterminated",
			input:    "\x9D0;title",
			expected: []string{"\x9D", "0", ";", "t", "i", "t", "l", "e"},
		},
		{
			name:     "C1 DCS unterminated",
			input:    "\x90data",
			expected: []string{"\x90", "d", "a", "t", "a"},
		},
		{
			name:     "C1 SOS with C1 ST terminator",
			input:    "\x98hello\x9C",
			expected: []string{"\x98hello\x9C"},
		},
		{
			name:     "C1 PM with 7-bit ST terminator",
			input:    "\x9Emsg\x1b\\",
			expected: []string{"\x9E", "m", "s", "g", "\x1b\\"},
		},
		{
			name:     "C1 APC with C1 ST terminator",
			input:    "\x9Fdata\x9C",
			expected: []string{"\x9Fdata\x9C"},
		},
		{
			name:     "single ESC byte",
			input:    "\x1b",
			expected: []string{"\x1b"},
		},
		{
			name:     "single C1 control byte",
			input:    "\x84",
			expected: []string{"\x84"},
		},
		{
			name:     "UTF-8 cafe",
			input:    "café",
			expected: []string{"c", "a", "f", "é"},
		},
		{
			name:     "UTF-8 Japanese text",
			input:    "日本語",
			expected: []string{"日", "本", "語"},
		},
		{
			name:     "UTF-8 runes with continuation bytes in C1 range",
			input:    "Āğל",
			expected: []string{"Ā", "ğ", "ל"},
		},
		{
			name:     "mixed ANSI and UTF-8 adversarial payload",
			input:    "\x1b[31mĀğ日本語café\x1b[0m",
			expected: []string{"\x1b[31m", "Ā", "ğ", "日", "本", "語", "c", "a", "f", "é", "\x1b[0m"},
		},
		{
			name:     "SOS canceled by CAN",
			input:    "\x1bXhello\x18z",
			expected: []string{"\x1bXhello", "\x18", "z"},
		},
		{
			name:     "malformed CSI: param after intermediate",
			input:    "\x1b[ 1mok",
			expected: []string{"\x1b", "[", " ", "1", "m", "o", "k"},
		},
		{
			name:     "no ANSI at start",
			input:    "plain",
			expected: []string{"p", "l", "a", "i", "n"},
		},
	}

	runANSICases(t, tests, true, true)
}

func TestAnsiEscapeSequencesPureUTF8Parity(t *testing.T) {
	t.Parallel()

	samples := []string{
		"café",
		"日本語",
		"Āğל",
		"A\u0301",
		"👩🏽‍💻",
		"Résumé — 東京 — 👍",
	}

	collectString := func(input string, ansi7, ansi8 bool) []string {
		iter := graphemes.FromString(input)
		iter.AnsiEscapeSequences = ansi7
		iter.AnsiEscapeSequences8Bit = ansi8
		var out []string
		for iter.Next() {
			out = append(out, iter.Value())
		}
		return out
	}

	collectBytes := func(input string, ansi7, ansi8 bool) []string {
		iter := graphemes.FromBytes([]byte(input))
		iter.AnsiEscapeSequences = ansi7
		iter.AnsiEscapeSequences8Bit = ansi8
		var out []string
		for iter.Next() {
			out = append(out, string(iter.Value()))
		}
		return out
	}

	for i, sample := range samples {
		sample := sample
		t.Run("sample-"+string(rune('A'+i)), func(t *testing.T) {
			t.Parallel()

			stringBase := collectString(sample, false, false)
			for _, flags := range []struct {
				name  string
				ansi7 bool
				ansi8 bool
			}{
				{name: "7-bit only", ansi7: true, ansi8: false},
				{name: "8-bit only", ansi7: false, ansi8: true},
				{name: "both", ansi7: true, ansi8: true},
			} {
				gotString := collectString(sample, flags.ansi7, flags.ansi8)
				if !reflect.DeepEqual(stringBase, gotString) {
					t.Fatalf("string parity mismatch for %q (%s)\noff=%q\non=%q", sample, flags.name, stringBase, gotString)
				}

				bytesBase := collectBytes(sample, false, false)
				gotBytes := collectBytes(sample, flags.ansi7, flags.ansi8)
				if !reflect.DeepEqual(bytesBase, gotBytes) {
					t.Fatalf("bytes parity mismatch for %q (%s)\noff=%q\non=%q", sample, flags.name, bytesBase, gotBytes)
				}
			}
		})
	}
}

// ansiSample builds a string that mixes ANSI escape sequences with regular text,
// simulating realistic terminal output (colored words, resets, bold, etc.).
func ansiSample() string {
	var b strings.Builder

	// Simulate `ls --color=auto`-style output: colored filenames with resets
	colors := []string{
		"\x1b[1;34m", // bold blue (directories)
		"\x1b[0;32m", // green (executables)
		"\x1b[0;36m", // cyan (symlinks)
		"\x1b[1;31m", // bold red (errors)
		"\x1b[33m",   // yellow (warnings)
	}
	reset := "\x1b[0m"

	lines := []string{
		"drwxr-xr-x  5 user staff  160 Jan  1 12:00 Documents",
		"drwxr-xr-x  3 user staff   96 Feb  2 09:30 Downloads",
		"-rwxr-xr-x  1 user staff 8432 Mar 15 14:22 build.sh",
		"lrwxr-xr-x  1 user staff   11 Apr 20 08:00 config -> /etc/config",
		"-rw-r--r--  1 user staff 1024 May  5 16:45 README.md",
		"total 42",
		"drwxr-xr-x  2 user staff   64 Jun 10 11:11 src",
		"-rw-r--r--  1 user staff  512 Jul  7 07:07 main.go",
		"error: file not found: missing.txt",
		"warning: deprecated function used in line 42",
	}

	// Repeat to get a decent-sized sample
	for round := 0; round < 20; round++ {
		for i, line := range lines {
			color := colors[i%len(colors)]
			// OSC title update every 5 lines
			if i%5 == 0 {
				b.WriteString("\x1b]0;terminal - round ")
				b.WriteString(string(rune('0' + round%10)))
				b.WriteString("\x07")
			}
			b.WriteString(color)
			b.WriteString(line)
			b.WriteString(reset)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// ansiSample8Bit builds a string that uses 8-bit C1 initiators.
func ansiSample8Bit() string {
	var b strings.Builder

	lines := []string{
		"drwxr-xr-x  5 user staff  160 Jan  1 12:00 Documents",
		"drwxr-xr-x  3 user staff   96 Feb  2 09:30 Downloads",
		"-rwxr-xr-x  1 user staff 8432 Mar 15 14:22 build.sh",
		"lrwxr-xr-x  1 user staff   11 Apr 20 08:00 config -> /etc/config",
		"-rw-r--r--  1 user staff 1024 May  5 16:45 README.md",
		"total 42",
		"drwxr-xr-x  2 user staff   64 Jun 10 11:11 src",
		"-rw-r--r--  1 user staff  512 Jul  7 07:07 main.go",
		"error: file not found: missing.txt",
		"warning: deprecated function used in line 42",
	}

	for round := 0; round < 20; round++ {
		for i, line := range lines {
			// C1 OSC: 0x9D ... BEL
			if i%5 == 0 {
				b.WriteByte(0x9D)
				b.WriteString("0;terminal - round ")
				b.WriteString(string(rune('0' + round%10)))
				b.WriteByte(0x07)
			}
			// C1 CSI SGR: 0x9B ... m
			b.WriteByte(0x9B)
			b.WriteString("1;3")
			b.WriteString(string(rune('0' + (i % 8))))
			b.WriteByte('m')
			b.WriteString(line)
			// C1 CSI reset: 0x9B0m
			b.WriteByte(0x9B)
			b.WriteString("0m")
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// ansiSampleMixed builds a string with both 7-bit and 8-bit ANSI forms.
func ansiSampleMixed() string {
	var b strings.Builder
	a7 := ansiSample()
	a8 := ansiSample8Bit()
	b.WriteString(a7)
	b.WriteString(a8)
	return b.String()
}

// BenchmarkAnsiOption benchmarks the iterator on text that contains ANSI escapes,
// and on plain text, with the AnsiEscapeSequences option on and off.
func BenchmarkAnsiOption(b *testing.B) {
	ansi7 := ansiSample()
	ansi8 := ansiSample8Bit()
	ansiMixed := ansiSampleMixed()
	plain, err := testdata.Sample()
	if err != nil {
		b.Fatal(err)
	}
	plainStr := string(plain)

	b.Run("AnsiText7Bit/Option7BitOn", func(b *testing.B) {
		b.SetBytes(int64(len(ansi7)))
		for i := 0; i < b.N; i++ {
			iter := graphemes.FromString(ansi7)
			iter.AnsiEscapeSequences = true
			c := 0
			for iter.Next() {
				_ = iter.Value()
				c++
			}
			b.ReportMetric(float64(c), "tokens")
		}
	})

	b.Run("AnsiText7Bit/OptionOff", func(b *testing.B) {
		b.SetBytes(int64(len(ansi7)))
		for i := 0; i < b.N; i++ {
			iter := graphemes.FromString(ansi7)
			c := 0
			for iter.Next() {
				_ = iter.Value()
				c++
			}
			b.ReportMetric(float64(c), "tokens")
		}
	})

	b.Run("AnsiText8Bit/Option8BitOn", func(b *testing.B) {
		b.SetBytes(int64(len(ansi8)))
		for i := 0; i < b.N; i++ {
			iter := graphemes.FromString(ansi8)
			iter.AnsiEscapeSequences8Bit = true
			c := 0
			for iter.Next() {
				_ = iter.Value()
				c++
			}
			b.ReportMetric(float64(c), "tokens")
		}
	})

	b.Run("AnsiText8Bit/OptionOff", func(b *testing.B) {
		b.SetBytes(int64(len(ansi8)))
		for i := 0; i < b.N; i++ {
			iter := graphemes.FromString(ansi8)
			c := 0
			for iter.Next() {
				_ = iter.Value()
				c++
			}
			b.ReportMetric(float64(c), "tokens")
		}
	})

	b.Run("AnsiTextMixed/BothOptionsOn", func(b *testing.B) {
		b.SetBytes(int64(len(ansiMixed)))
		for i := 0; i < b.N; i++ {
			iter := graphemes.FromString(ansiMixed)
			iter.AnsiEscapeSequences = true
			iter.AnsiEscapeSequences8Bit = true
			c := 0
			for iter.Next() {
				_ = iter.Value()
				c++
			}
			b.ReportMetric(float64(c), "tokens")
		}
	})

	b.Run("PlainText/Option7BitOn", func(b *testing.B) {
		b.SetBytes(int64(len(plainStr)))
		for i := 0; i < b.N; i++ {
			iter := graphemes.FromString(plainStr)
			iter.AnsiEscapeSequences = true
			c := 0
			for iter.Next() {
				_ = iter.Value()
				c++
			}
			b.ReportMetric(float64(c), "tokens")
		}
	})

	b.Run("PlainText/Option8BitOn", func(b *testing.B) {
		b.SetBytes(int64(len(plainStr)))
		for i := 0; i < b.N; i++ {
			iter := graphemes.FromString(plainStr)
			iter.AnsiEscapeSequences8Bit = true
			c := 0
			for iter.Next() {
				_ = iter.Value()
				c++
			}
			b.ReportMetric(float64(c), "tokens")
		}
	})

	b.Run("PlainText/BothOptionsOn", func(b *testing.B) {
		b.SetBytes(int64(len(plainStr)))
		for i := 0; i < b.N; i++ {
			iter := graphemes.FromString(plainStr)
			iter.AnsiEscapeSequences = true
			iter.AnsiEscapeSequences8Bit = true
			c := 0
			for iter.Next() {
				_ = iter.Value()
				c++
			}
			b.ReportMetric(float64(c), "tokens")
		}
	})

	b.Run("PlainText/OptionOff", func(b *testing.B) {
		b.SetBytes(int64(len(plainStr)))
		for i := 0; i < b.N; i++ {
			iter := graphemes.FromString(plainStr)
			c := 0
			for iter.Next() {
				_ = iter.Value()
				c++
			}
			b.ReportMetric(float64(c), "tokens")
		}
	})
}
