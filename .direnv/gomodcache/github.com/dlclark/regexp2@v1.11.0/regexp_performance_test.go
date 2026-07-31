package regexp2

import (
	"strings"
	"testing"
	"time"
)

func BenchmarkLiteral(b *testing.B) {
	x := strings.Repeat("x", 50) + "y"
	b.StopTimer()
	re := MustCompile("y", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchString(x); !m || err != nil {
			b.Fatalf("no match or error! %v", err)
		}
	}
}

func BenchmarkNotLiteral(b *testing.B) {
	x := strings.Repeat("x", 50) + "y"
	b.StopTimer()
	re := MustCompile(".y", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchString(x); !m || err != nil {
			b.Fatalf("no match or error! %v", err)
		}
	}
}

func BenchmarkMatchClass(b *testing.B) {
	b.StopTimer()
	x := strings.Repeat("xxxx", 20) + "w"
	re := MustCompile("[abcdw]", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchString(x); !m || err != nil {
			b.Fatalf("no match or error! %v", err)
		}

	}
}

func BenchmarkMatchClass_InRange(b *testing.B) {
	b.StopTimer()
	// 'b' is between 'a' and 'c', so the charclass
	// range checking is no help here.
	x := strings.Repeat("bbbb", 20) + "c"
	re := MustCompile("[ac]", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchString(x); !m || err != nil {
			b.Fatalf("no match or error! %v", err)
		}
	}
}

/*
func BenchmarkReplaceAll(b *testing.B) {

	x := "abcdefghijklmnopqrstuvwxyz"
	b.StopTimer()
	re := MustCompile("[cjrw]", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		re.ReplaceAllString(x, "")
	}

}
*/
func BenchmarkAnchoredLiteralShortNonMatch(b *testing.B) {
	b.StopTimer()
	x := "abcdefghijklmnopqrstuvwxyz"
	re := MustCompile("^zbc(d|e)", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchString(x); m || err != nil {
			b.Fatalf("unexpected match or error! %v", err)
		}
	}
}

func BenchmarkAnchoredLiteralLongNonMatch(b *testing.B) {
	b.StopTimer()

	data := "abcdefghijklmnopqrstuvwxyz"
	x := make([]rune, 32768*len(data))
	for i := 0; i < 32768; /*(2^15)*/ i++ {
		for j := 0; j < len(data); j++ {
			x[i*len(data)+j] = rune(data[j])
		}
	}

	re := MustCompile("^zbc(d|e)", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchRunes(x); m || err != nil {
			b.Fatalf("unexpected match or error! %v", err)
		}
	}
}

func BenchmarkAnchoredShortMatch(b *testing.B) {
	b.StopTimer()
	x := "abcdefghijklmnopqrstuvwxyz"
	re := MustCompile("^.bc(d|e)", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchString(x); !m || err != nil {
			b.Fatalf("no match or error! %v", err)
		}
	}
}

func BenchmarkAnchoredLongMatch(b *testing.B) {
	b.StopTimer()
	data := "abcdefghijklmnopqrstuvwxyz"
	x := make([]rune, 32768*len(data))
	for i := 0; i < 32768; /*(2^15)*/ i++ {
		for j := 0; j < len(data); j++ {
			x[i*len(data)+j] = rune(data[j])
		}
	}

	re := MustCompile("^.bc(d|e)", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchRunes(x); !m || err != nil {
			b.Fatalf("no match or error! %v", err)
		}
	}
}

func BenchmarkOnePassShortA(b *testing.B) {
	b.StopTimer()
	x := "abcddddddeeeededd"
	re := MustCompile("^.bc(d|e)*$", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchString(x); !m || err != nil {
			b.Fatalf("no match or error! %v", err)
		}
	}
}

func BenchmarkNotOnePassShortA(b *testing.B) {
	b.StopTimer()
	x := "abcddddddeeeededd"
	re := MustCompile(".bc(d|e)*$", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchString(x); !m || err != nil {
			b.Fatalf("no match or error! %v", err)
		}
	}
}

func BenchmarkOnePassShortB(b *testing.B) {
	b.StopTimer()
	x := "abcddddddeeeededd"
	re := MustCompile("^.bc(?:d|e)*$", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchString(x); !m || err != nil {
			b.Fatalf("no match or error! %v", err)
		}
	}
}

func BenchmarkNotOnePassShortB(b *testing.B) {
	b.StopTimer()
	x := "abcddddddeeeededd"
	re := MustCompile(".bc(?:d|e)*$", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchString(x); !m || err != nil {
			b.Fatalf("no match or error! %v", err)
		}
	}
}

func BenchmarkOnePassLongPrefix(b *testing.B) {
	b.StopTimer()
	x := "abcdefghijklmnopqrstuvwxyz"
	re := MustCompile("^abcdefghijklmnopqrstuvwxyz.*$", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchString(x); !m || err != nil {
			b.Fatalf("no match or error! %v", err)
		}
	}
}

func BenchmarkOnePassLongNotPrefix(b *testing.B) {
	b.StopTimer()
	x := "abcdefghijklmnopqrstuvwxyz"
	re := MustCompile("^.bcdefghijklmnopqrstuvwxyz.*$", 0)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := re.MatchString(x); !m || err != nil {
			b.Fatalf("no match or error! %v", err)
		}
	}
}

var text []rune

func makeText(n int) []rune {
	if len(text) >= n {
		return text[:n]
	}
	text = make([]rune, n)
	x := ^uint32(0)
	for i := range text {
		x += x
		x ^= 1
		if int32(x) < 0 {
			x ^= 0x88888eef
		}
		if x%31 == 0 {
			text[i] = '\n'
		} else {
			text[i] = rune(x%(0x7E+1-0x20) + 0x20)
		}
	}
	return text
}

func benchmark(b *testing.B, re string, n int) {
	r := MustCompile(re, 0)
	t := makeText(n)
	b.ResetTimer()
	b.SetBytes(int64(n))
	for i := 0; i < b.N; i++ {
		if m, err := r.MatchRunes(t); m {
			b.Fatal("match!")
		} else if err != nil {
			b.Fatalf("Err %v", err)
		}
	}
}

const (
	easy0  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ$"
	easy1  = "A[AB]B[BC]C[CD]D[DE]E[EF]F[FG]G[GH]H[HI]I[IJ]J$"
	medium = "[XYZ]ABCDEFGHIJKLMNOPQRSTUVWXYZ$"
	hard   = "[ -~]*ABCDEFGHIJKLMNOPQRSTUVWXYZ$"
	hard1  = "ABCD|CDEF|EFGH|GHIJ|IJKL|KLMN|MNOP|OPQR|QRST|STUV|UVWX|WXYZ"
	parens = "([ -~])*(A)(B)(C)(D)(E)(F)(G)(H)(I)(J)(K)(L)(M)" +
		"(N)(O)(P)(Q)(R)(S)(T)(U)(V)(W)(X)(Y)(Z)$"
)

func BenchmarkMatchEasy0_32(b *testing.B)   { benchmark(b, easy0, 32<<0) }
func BenchmarkMatchEasy0_1K(b *testing.B)   { benchmark(b, easy0, 1<<10) }
func BenchmarkMatchEasy0_32K(b *testing.B)  { benchmark(b, easy0, 32<<10) }
func BenchmarkMatchEasy0_1M(b *testing.B)   { benchmark(b, easy0, 1<<20) }
func BenchmarkMatchEasy0_32M(b *testing.B)  { benchmark(b, easy0, 32<<20) }
func BenchmarkMatchEasy1_32(b *testing.B)   { benchmark(b, easy1, 32<<0) }
func BenchmarkMatchEasy1_1K(b *testing.B)   { benchmark(b, easy1, 1<<10) }
func BenchmarkMatchEasy1_32K(b *testing.B)  { benchmark(b, easy1, 32<<10) }
func BenchmarkMatchEasy1_1M(b *testing.B)   { benchmark(b, easy1, 1<<20) }
func BenchmarkMatchEasy1_32M(b *testing.B)  { benchmark(b, easy1, 32<<20) }
func BenchmarkMatchMedium_32(b *testing.B)  { benchmark(b, medium, 32<<0) }
func BenchmarkMatchMedium_1K(b *testing.B)  { benchmark(b, medium, 1<<10) }
func BenchmarkMatchMedium_32K(b *testing.B) { benchmark(b, medium, 32<<10) }
func BenchmarkMatchMedium_1M(b *testing.B)  { benchmark(b, medium, 1<<20) }
func BenchmarkMatchMedium_32M(b *testing.B) { benchmark(b, medium, 32<<20) }
func BenchmarkMatchHard_32(b *testing.B)    { benchmark(b, hard, 32<<0) }
func BenchmarkMatchHard_1K(b *testing.B)    { benchmark(b, hard, 1<<10) }
func BenchmarkMatchHard_32K(b *testing.B)   { benchmark(b, hard, 32<<10) }
func BenchmarkMatchHard_1M(b *testing.B)    { benchmark(b, hard, 1<<20) }
func BenchmarkMatchHard_32M(b *testing.B)   { benchmark(b, hard, 32<<20) }
func BenchmarkMatchHard1_32(b *testing.B)   { benchmark(b, hard1, 32<<0) }
func BenchmarkMatchHard1_1K(b *testing.B)   { benchmark(b, hard1, 1<<10) }
func BenchmarkMatchHard1_32K(b *testing.B)  { benchmark(b, hard1, 32<<10) }
func BenchmarkMatchHard1_1M(b *testing.B)   { benchmark(b, hard1, 1<<20) }
func BenchmarkMatchHard1_32M(b *testing.B)  { benchmark(b, hard1, 32<<20) }

// TestProgramTooLongForBacktrack tests that a regex which is too long
// for the backtracker still executes properly.
func TestProgramTooLongForBacktrack(t *testing.T) {
	longRegex := MustCompile(`(one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty|twentyone|twentytwo|twentythree|twentyfour|twentyfive|twentysix|twentyseven|twentyeight|twentynine|thirty|thirtyone|thirtytwo|thirtythree|thirtyfour|thirtyfive|thirtysix|thirtyseven|thirtyeight|thirtynine|forty|fortyone|fortytwo|fortythree|fortyfour|fortyfive|fortysix|fortyseven|fortyeight|fortynine|fifty|fiftyone|fiftytwo|fiftythree|fiftyfour|fiftyfive|fiftysix|fiftyseven|fiftyeight|fiftynine|sixty|sixtyone|sixtytwo|sixtythree|sixtyfour|sixtyfive|sixtysix|sixtyseven|sixtyeight|sixtynine|seventy|seventyone|seventytwo|seventythree|seventyfour|seventyfive|seventysix|seventyseven|seventyeight|seventynine|eighty|eightyone|eightytwo|eightythree|eightyfour|eightyfive|eightysix|eightyseven|eightyeight|eightynine|ninety|ninetyone|ninetytwo|ninetythree|ninetyfour|ninetyfive|ninetysix|ninetyseven|ninetyeight|ninetynine|onehundred)`, 0)

	if m, err := longRegex.MatchString("two"); !m {
		t.Errorf("longRegex.MatchString(\"two\") was false, want true")
	} else if err != nil {
		t.Errorf("Error: %v", err)
	}
	if m, err := longRegex.MatchString("xxx"); m {
		t.Errorf("longRegex.MatchString(\"xxx\") was true, want false")
	} else if err != nil {
		t.Errorf("Error: %v", err)
	}
}

func BenchmarkLeading(b *testing.B) {
	b.StopTimer()
	r := MustCompile("[a-q][^u-z]{13}x", 0)
	inp := makeText(1000000)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if m, err := r.MatchRunes(inp); !m {
			b.Errorf("Expected match")
		} else if err != nil {
			b.Errorf("Error: %v", err)
		}
	}
}

func BenchmarkShortSearch(b *testing.B) {
	// set to default check period for benchmarking purposes
	// unit tests tend to send this low
	SetTimeoutCheckPeriod(DefaultClockPeriod)
	type benchmark struct {
		name     string
		parallel bool // Run in parallel?
		timeout  time.Duration
		increase time.Duration // timeout increase per match
	}
	for _, mode := range []benchmark{
		{"serial-no-timeout", false, DefaultMatchTimeout, 0},
		{"serial-fixed-timeout", false, time.Second, 0},
		{"serial-increasing-timeout", false, time.Second, time.Second},
		{"parallel-no-timeout", true, DefaultMatchTimeout, 0},
		{"parallel-fixed-timeout", true, time.Second, 0},
		{"parallel-increasing-timeout", true, time.Second, time.Second},
	} {
		b.Run(mode.name, func(b *testing.B) {
			t := makeText(100)
			b.SetBytes(int64(len(t)))
			matchOnce := func(r *Regexp) {
				if m, err := r.MatchRunes(t); m {
					b.Fatal("match!")
				} else if err != nil {
					b.Fatalf("Err %v", err)
				}
			}

			if !mode.parallel {
				r := MustCompile(easy0, 0)
				r.MatchTimeout = mode.timeout
				for i := 0; i < b.N; i++ {
					matchOnce(r)
					r.MatchTimeout += mode.increase
				}
			} else {
				b.RunParallel(func(pb *testing.PB) {
					r := MustCompile(easy0, 0)
					r.MatchTimeout = mode.timeout
					for pb.Next() {
						matchOnce(r)
						r.MatchTimeout += mode.increase
					}
				})
			}
		})
	}
}
