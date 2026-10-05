package termrender

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Under either count, a cut or a wrapped row never comes out wider than
// asked, whatever clusters the text holds, and keeps its escape sequences.
func TestCutsKeepToTheCount(t *testing.T) {
	t.Cleanup(func() { SetCells(ansi.GraphemeWidth) })
	for _, m := range []ansi.Method{ansi.WcWidth, ansi.GraphemeWidth} {
		SetCells(m)
		for _, s := range []string{
			strings.Repeat("👨\u200d👩\u200d👧", 5),
			strings.Repeat("🇨🇳", 8),
			"\x1b[1mab中文👩🏽\u200d💻cd\x1b[m" + strings.Repeat("1\ufe0f\u20e3", 6),
			strings.Repeat("x1\ufe0f\u20e3 ", 9),
		} {
			for w := 1; w <= 14; w++ {
				if got := Truncate(s, w, "…"); m.StringWidth(got) > w {
					t.Fatalf("%v Truncate(%q, %d) is %d wide: %q", m, s, w, m.StringWidth(got), got)
				}
				if c := Cut(s, 2, 2+w); m.StringWidth(c) > w {
					t.Fatalf("%v Cut(%q, 2, %d) is %d wide: %q", m, s, 2+w, m.StringWidth(c), c)
				}
				for row := range strings.SplitSeq(Hardwrap(s, w), "\n") {
					if m.StringWidth(row) > max(w, 2) {
						t.Fatalf("%v Hardwrap(%q, %d) has a row %d wide: %q", m, s, w, m.StringWidth(row), row)
					}
				}
			}
		}
	}
	if got := Truncate("\x1b[1mabc\x1b[m", 2, ""); got != "\x1b[1mab\x1b[m" {
		t.Fatalf("Truncate dropped an escape sequence: %q", got)
	}
}

// A tool line holding clusters stays one row as wide as asked under either
// count, so its closing parenthesis never lands in the scrollbar column.
func TestToolCardFitsEitherCount(t *testing.T) {
	t.Cleanup(func() { SetCells(ansi.GraphemeWidth) })
	cmd := "printf '\\033[31m红色\\033[0m\\t列二\\t列三\\n名字\\t✅ 完成\\t⚠\ufe0f 警告\\t👨\u200d👩\u200d👧 🇨🇳 1\ufe0f\u20e3 ◆ ●\\n' > 输出.txt; cat 输出.txt"
	b, _ := json.Marshal(map[string]string{"command": cmd})
	arg := string(b)
	for _, m := range []ansi.Method{ansi.WcWidth, ansi.GraphemeWidth} {
		SetCells(m)
		for w := 20; w <= 120; w++ {
			if n := m.StringWidth(ansi.Strip(ToolCard("bash", arg, w))); n > w {
				t.Fatalf("%v ToolCard at %d is %d wide", m, w, n)
			}
		}
	}
}

// Per rune, a combining mark takes no column, so a cut ending right after its
// base keeps it: a decomposed name is copied and shown whole.
func TestCutKeepsACombiningMarkAtTheBoundary(t *testing.T) {
	SetCells(ansi.WcWidth)
	t.Cleanup(func() { SetCells(ansi.GraphemeWidth) })
	const s = "cafe\u0301 ok"
	if got := Cut(s, 0, 4); got != "cafe\u0301" {
		t.Fatalf("Cut(%q, 0, 4) = %q, want the mark kept", s, got)
	}
	if got := Truncate(s, 4, ""); got != "cafe\u0301" {
		t.Fatalf("Truncate(%q, 4) = %q, want the mark kept", s, got)
	}
	if got := Truncate(s, 5, "…"); got != "cafe\u0301…" {
		t.Fatalf("Truncate(%q, 5, …) = %q, want the mark before the tail", s, got)
	}
}
