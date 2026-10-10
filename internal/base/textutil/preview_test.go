package textutil

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

var testLimit = PreviewLimit{Graphemes: 20, Lines: 3}

func TestBoundProseLeavesOrdinaryTextAlone(t *testing.T) {
	for _, s := range []string{
		"", "Reviews pull requests", "审查代码并给出修改建议", "点検 / 레이아웃 / Привет",
		"line one\nline two", "tab\there", "🚀 launch 👨\u200d👩\u200d👧\u200d👦 family 🇨🇳", "café", "فارسی می\u200cشود",
		"नमस्ते क्\u200dष",
	} {
		got, cut := BoundProse(s, PreviewLimit{Graphemes: 200, Lines: 5})
		if got != s || cut {
			t.Errorf("BoundProse(%q) = %q, %v", s, got, cut)
		}
	}
}

func TestBoundProseRemovesHiddenText(t *testing.T) {
	cases := map[string]string{
		"esc csi":      "a\x1b[31mred\x1b[0mb",
		"esc osc":      "a\x1b]0;title\x07b",
		"lone esc":     "a\x1bb",
		"c1 csi":       "a\u009b31mb",
		"c1 other":     "a\u0085\u0090\u009cb",
		"nul and bel":  "a\x00\x07\x7fb",
		"bidi":         "a\u202e\u202a\u202b\u202c\u202d\u2066\u2067\u2068\u2069\u200e\u200f\u061cb",
		"zero width":   "a\u200b\u2060\ufeff\u00adb",
		"tags":         "a\U000e0041\U000e007fb",
		"fillers":      "a\u3164\u115f\u1160\uffa0\u034f\u180eb",
		"lone joiners": "a\u200d\u200cb",
		"cjk joiner":   "中\u200d文",
	}
	want := map[string]string{"c1 other": "a\nb", "c1 csi": "a31mb"}
	for name, in := range cases {
		got, cut := BoundProse(in, PreviewLimit{Graphemes: 50, Lines: 3})
		w := "ab"
		if name == "lone esc" {
			w = "a"
		}
		if v, ok := want[name]; ok {
			w = v
		}
		if name == "cjk joiner" {
			w = "中文"
		}
		if name == "esc csi" {
			w = "aredb"
		}
		if got != w || !cut {
			t.Errorf("%s: BoundProse(%q) = %q, %v; want %q (removal is flagged)", name, in, got, cut, w)
		}
	}
}

func TestBoundProseLineBreaks(t *testing.T) {
	got, cut := BoundProse("a\r\nb\rc\u2028d\u2029e", PreviewLimit{Graphemes: 50, Lines: 10})
	if got != "a\nb\nc\nd\ne" || cut {
		t.Fatalf("got %q, %v", got, cut)
	}
	got, cut = BoundProse("1\n2\n3\n4\n5", PreviewLimit{Graphemes: 50, Lines: 3})
	if got != "1\n2\n3"+PreviewMarker || !cut {
		t.Fatalf("got %q, %v", got, cut)
	}
	got, cut = BoundProse("1\n2\n3\n\n\n", PreviewLimit{Graphemes: 50, Lines: 3})
	if got != "1\n2\n3" || cut {
		t.Fatalf("trailing blank lines are not content: %q, %v", got, cut)
	}
	got, cut = BoundProse(strings.Repeat("\n", 5000)+"hidden", PreviewLimit{Graphemes: 50, Lines: 3})
	if !cut || strings.Contains(got, "hidden") {
		t.Fatalf("blank padding must not push text into view as complete: %q, %v", got, cut)
	}
}

func TestBoundProseKeepsEmojiJoinersAndScriptJoiners(t *testing.T) {
	for _, s := range []string{"👨\u200d👩\u200d👧", "می\u200cشود", "क्\u200dष"} {
		if got, _ := BoundProse(s, testLimit); got != s {
			t.Errorf("BoundProse(%q) = %q", s, got)
		}
	}
	if got, _ := BoundProse("\u200d👨", testLimit); got != "👨" {
		t.Errorf("leading joiner kept: %q", got)
	}
}

func TestBoundProseTruncationNeverSplitsGraphemes(t *testing.T) {
	for name, unit := range map[string]string{
		"ascii": "a", "cjk": "汉", "emoji": "😀", "zwj family": "👨\u200d👩\u200d👧\u200d👦",
		"combining": "é̂", "flag": "🇨🇳", "skin": "👍🏽", "hangul jamo": "한",
	} {
		s := strings.Repeat(unit, 40)
		got, cut := BoundProse(s, PreviewLimit{Graphemes: 7, Lines: 1})
		if !cut || !utf8.ValidString(got) || !strings.HasSuffix(got, PreviewMarker) {
			t.Fatalf("%s: got %q cut=%v", name, got, cut)
		}
		body := strings.TrimSuffix(got, PreviewMarker)
		if n := uniseg.GraphemeClusterCount(got); n > 7 {
			t.Errorf("%s: %d clusters exceed the limit", name, n)
		}
		if body != strings.Repeat(unit, 6) {
			t.Errorf("%s: kept %q, a cluster was split or miscounted", name, body)
		}
	}
}

func TestBoundProseFlagMeansTextIsCutOrRemoved(t *testing.T) {
	lim := PreviewLimit{Graphemes: 5, Lines: 2}
	for in, wantCut := range map[string]bool{
		"12345": false, "123456": true, "\u200b12345": true, "12\n34": false, "1\n2\n3": true,
		"12345\x1b[0m": true, "1\n2\n\n": false, "ab\u202ec": true,
	} {
		if _, cut := BoundProse(in, lim); cut != wantCut {
			t.Errorf("BoundProse(%q) cut = %v, want %v", in, cut, wantCut)
		}
	}
}

func TestBoundProseHugeInputIsBounded(t *testing.T) {
	in := strings.Repeat("\u200b", 5<<20) + "tail"
	got, cut := BoundProse(in, testLimit)
	if !cut || len(got) > 200 {
		t.Fatalf("len=%d cut=%v", len(got), cut)
	}
	in = strings.Repeat("x", 5<<20)
	if got, cut = BoundProse(in, testLimit); !cut || got != strings.Repeat("x", 19)+PreviewMarker {
		t.Fatalf("got %q cut=%v", got, cut)
	}
	if got, cut = BoundProse("\xff\xfe"+strings.Repeat("好", 1<<20), testLimit); !cut || !utf8.ValidString(got) {
		t.Fatalf("invalid utf8 survived: %q", got)
	}
}

func TestBoundProseIsDeterministicAndIdempotent(t *testing.T) {
	in := "x\x1b[1m" + strings.Repeat("字\u202e", 100) + "\n\n\n\nz"
	a, ac := BoundProse(in, testLimit)
	b, bc := BoundProse(in, testLimit)
	if a != b || ac != bc {
		t.Fatal("not deterministic")
	}
	if again, cut := BoundProse(a, testLimit); again != a || cut {
		t.Fatalf("not idempotent: %q -> %q, %v", a, again, cut)
	}
}

func TestBoundLiteralShowsWhatIsThere(t *testing.T) {
	cases := map[string]string{
		"run\x1b[2Jme":    `run\u{1b}[2Jme`,
		"a\nb":            `a\u{a}b`,
		"a\tb\r":          `a\u{9}b\u{d}`,
		"x\u202ey":        `x\u{202e}y`,
		"x\u200by":        `x\u{200b}y`,
		"x\x00y":          `x\u{0}y`,
		"x\u009by":        `x\u{9b}y`,
		"npx -y 包 🚀":      "npx -y 包 🚀",
		"https://例え.jp/a": "https://例え.jp/a",
	}
	for in, want := range cases {
		got, cut := BoundLiteral(in, PreviewLimit{Graphemes: 50, Lines: 1})
		if got != want || cut {
			t.Errorf("BoundLiteral(%q) = %q, %v; want %q", in, got, cut, want)
		}
	}
}

func TestBoundLiteralTruncates(t *testing.T) {
	got, cut := BoundLiteral(strings.Repeat("é", 30), PreviewLimit{Graphemes: 4, Lines: 1})
	if !cut || got != strings.Repeat("é", 3)+PreviewMarker {
		t.Fatalf("got %q cut=%v", got, cut)
	}
	if got, cut := BoundLiteral("1234", PreviewLimit{Graphemes: 4, Lines: 1}); got != "1234" || cut {
		t.Fatalf("exact fit: %q %v", got, cut)
	}
	if got, cut := BoundLiteral("x", PreviewLimit{Graphemes: 0, Lines: 0}); got != "x" || cut {
		t.Fatalf("zero limit: %q %v", got, cut)
	}
}

func TestBoundLiteralEscapesLineSeparators(t *testing.T) {
	got, cut := BoundLiteral("a b c\u0085d", PreviewLimit{Graphemes: 50, Lines: 1})
	if got != `a\u{2028}b\u{2029}c\u{85}d` || cut {
		t.Fatalf("got %q %v", got, cut)
	}
}

func TestInvisibleAndPrivateRunesAreHandledInBothModes(t *testing.T) {
	for name, r := range map[string]string{
		"braille blank": "⠀", "vs1": "︀", "vs15": "︎", "vs supplement": "\U000e0100", "vs supplement end": "\U000e01ef",
		"private bmp": "", "private bmp end": "", "private plane 15": "\U000f0000", "private plane 16": "\U0010fffd",
	} {
		prose, _ := BoundProse("a"+r+"b", testLimit)
		if prose != "ab" {
			t.Errorf("%s: prose %q", name, prose)
		}
		lit, _ := BoundLiteral("a"+r+"b", testLimit)
		if !strings.Contains(lit, `\u{`) || strings.ContainsRune(lit, []rune(r)[0]) {
			t.Errorf("%s: literal %q", name, lit)
		}
	}
}

func TestEmojiPresentationSelectorsSurvive(t *testing.T) {
	for _, s := range []string{"❤️", "☺️ ok", "1️⃣", "#️⃣", "👍🏽", "❤️‍🔥"} {
		if got, _ := BoundProse(s, testLimit); got != s {
			t.Errorf("BoundProse(%q) = %q", s, got)
		}
		if got, _ := BoundLiteral(s, testLimit); got != s {
			t.Errorf("BoundLiteral(%q) = %q", s, got)
		}
	}
	if got, _ := BoundProse("a️", testLimit); got != "a" {
		t.Errorf("selector after plain letter kept: %q", got)
	}
}

func TestStackedCombiningMarksAreCapped(t *testing.T) {
	zalgo := "e" + strings.Repeat("̖́", 40) + "x"
	prose, _ := BoundProse(zalgo, PreviewLimit{Graphemes: 50, Lines: 1})
	if want := "e" + strings.Repeat("̖́", 3) + "x"; prose != want {
		t.Fatalf("prose %q", prose)
	}
	lit, _ := BoundLiteral(zalgo, PreviewLimit{Graphemes: 400, Lines: 1})
	if strings.Count(lit, "́")+strings.Count(lit, "̖") != maxMarksPerBase {
		t.Fatalf("literal kept %q", lit)
	}
	if !strings.Contains(lit, `\u{301}`) {
		t.Fatalf("excess marks must be shown, not hidden: %q", lit)
	}
	for _, s := range []string{"việt", "ก็", "ךְִֵ", "أَبْجَدِيَّة"} {
		if got, _ := BoundProse(s, testLimit); got != s {
			t.Errorf("ordinary marks changed: %q -> %q", s, got)
		}
	}
	again, _ := BoundProse("e"+strings.Repeat("́", 7)+"é", testLimit)
	if again != "e"+strings.Repeat("́", 6)+"é" {
		t.Fatalf("run must reset at the next base: %q", again)
	}
}

func TestBoundLiteralTypedEscapeDiffersFromTheCharacter(t *testing.T) {
	typed, _ := BoundLiteral(`a\u{200b}`, PreviewLocator)
	real, _ := BoundLiteral("a\u200b", PreviewLocator)
	if typed == real {
		t.Fatalf("both render as %q", typed)
	}
	if again, _ := BoundLiteral(`C:\Users\x`, PreviewLocator); again != `C:\Users\x` {
		t.Fatalf("ordinary backslashes changed: %q", again)
	}
}

func TestBoundProseFlagsEveryDeletion(t *testing.T) {
	for name, in := range map[string]string{
		"unterminated OSC": "head\x1b]tail",
		"unterminated DCS": "head\x1bPtail",
		"complete CSI":     "he\x1b[31mad",
		"zero width":       "he\u200bad",
		"control":          "he\x00ad",
		"bidi":             "he\u202ead",
	} {
		if out, cut := BoundProse(in, PreviewProse); !cut || strings.ContainsAny(out, "\x1b\x00\u200b\u202e") {
			t.Errorf("%s: out=%q cut=%v", name, out, cut)
		}
	}
	for name, in := range map[string]string{
		"plain": "head tail", "crlf": "a\r\nb", "lone cr": "a\rb", "separator": "a\u2028b", "tab": "a\tb", "joiner in script": "क्‍ष",
	} {
		if _, cut := BoundProse(in, PreviewProse); cut {
			t.Errorf("%s flagged although nothing was removed", name)
		}
	}
}
