package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/contract/eventwire"
)

// conhostCJKCells is what a legacy conhost window with the default Chinese
// console font measured through WriteConsoleW + cursor read-back (#10540).
var conhostCJKCells = map[rune]int{
	'·': 2, '…': 2, '“': 2, '”': 2, '‘': 2, '’': 2, '—': 2, '–': 2,
	'●': 2, '○': 2, '←': 2, '↑': 2, '→': 2, '↓': 2, '※': 2, '★': 2,
	'■': 2, '◆': 2, 'α': 2, 'β': 2, 'Ω': 2, 'Ж': 2, 'ж': 2, '°': 2,
	'±': 2, '×': 2, '€': 2,
	'─': 1, '│': 1, '┌': 1, '•': 1, '█': 1, 'ü': 1, 'é': 1, '™': 1,
	'￩': 1, '￪': 1, '￫': 1, '￬': 1, '￭': 1, '￮': 1, '￨': 1,
}

// conhostBestFit is the same machine's WideCharToMultiByte(20127) answer.
var conhostBestFit = map[rune]rune{
	'·': '.', '…': '.', '“': '"', '”': '"', '‘': '\'', '’': '\'', '—': '-', '–': '-',
	'•': '.', 'ü': 'u', 'é': 'e', '™': 'T', '×': 'x',
}

// conhostFontLacks is what the default Chinese console font has no glyph for,
// besides every rune outside the Basic Multilingual Plane.
var conhostFontLacks = map[rune]bool{
	'✅': true, '❌': true, '⚠': true, '✔': true, '✓': true, '⎿': true, '❯': true,
	'⧗': true, '⟲': true, '☕': true, '❤': true, '☀': true, '⣾': true, '\u200d': true,
	'\ufe0f': true, '\u20e3': true,
}

func conhostHasGlyph(r rune) bool { return r <= 0xffff && !conhostFontLacks[r] }

func conhostTableFit() *glyphFit {
	return newGlyphFit(conhostCellsOf, func(r rune) rune { return conhostBestFit[r] }, conhostHasGlyph)
}

func conhostCellsOf(r rune) int {
	if w, ok := conhostCJKCells[r]; ok {
		return w
	}
	if r < 0x80 {
		return 1
	}
	return ansi.StringWidth(string(r))
}

func conhostRowCells(row string) int {
	n := 0
	for _, r := range ansi.Strip(row) {
		n += conhostCellsOf(r)
	}
	return n
}

func TestFrameFitsConhostMeasuredWidths(t *testing.T) {
	const cols = 120
	m, _ := testModel(t)
	m.glyphs = conhostTableFit()
	m.Update(tea.WindowSizeMsg{Width: cols, Height: 30})
	m.tr.AddUser("go")
	apply(m, eventwire.Event{Kind: "turn_started"})
	apply(m, eventwire.Event{Kind: "text", Text: strings.Repeat("“引号”…—→·", 12) + "\n"})
	apply(m, eventwire.Event{Kind: "message", Text: ""}, eventwire.Event{Kind: "turn_done"})

	frame := m.View().Content
	for i, row := range strings.Split(frame, "\n") {
		if got := conhostRowCells(row); got > cols {
			t.Fatalf("row %d draws %d cells on conhost, terminal has %d:\n%s", i, got, cols, ansi.Strip(row))
		}
	}
	if !strings.Contains(ansi.Strip(frame), `"引号".->.`) {
		t.Fatalf("the answer should carry the measured stand-ins:\n%s", ansi.Strip(frame))
	}
}

func TestGlyphFitLeavesEscapePayloadsAlone(t *testing.T) {
	link := ansi.SetHyperlink("https://example.com/·") + "a·b" + ansi.ResetHyperlink()
	got := conhostTableFit().apply(link)
	want := ansi.SetHyperlink("https://example.com/·") + "a.b" + ansi.ResetHyperlink()
	if got != want {
		t.Fatalf("apply(%q) = %q, want %q", link, got, want)
	}
}

func TestGlyphFitKeepsRunesTheConsoleAgreesOn(t *testing.T) {
	in := "│─┌ 中文 • ü"
	if got := conhostTableFit().apply(in); got != in {
		t.Fatalf("apply(%q) = %q, want it unchanged", in, got)
	}
}

// Every stand-in keeps the per-rune count the layout used and is drawn by the
// console as counted, in its own font; nothing falls to the unmapped '?'.
func TestConhostStandInsDrawEveryRuneAtItsCount(t *testing.T) {
	g := conhostTableFit()
	for _, r := range "◆●○★✅❌⚠✔✓⎿❯⧗⟲☕❤☀⣾🎉👨🏽🇨·…—→“" + "\u200d\ufe0f\u20e3" {
		got := g.fit(r)
		if a, b := ansi.WcWidth.StringWidth(string(r)), ansi.WcWidth.StringWidth(got); a != b {
			t.Fatalf("%U -> %q changed the per-rune count %d -> %d", r, got, a, b)
		}
		if strings.ContainsRune(got, '?') {
			t.Fatalf("%U -> %q falls back to the unmapped '?'", r, got)
		}
		for _, c := range got {
			if c >= 0x80 && !conhostDraws(c) {
				t.Fatalf("%U -> %q carries %U, which the console cannot draw as counted", r, got, c)
			}
		}
	}
	for r, want := range map[rune]string{'◆': "*", '✅': "√ ", '⚠': "!", '⎿': "└", '🇨': "C", '🎉': "  ", '\u200d': "", '×': "x"} {
		if got := g.fit(r); got != want {
			t.Fatalf("%U -> %q, want %q", r, got, want)
		}
	}
}

// A conhost frame carries only runes the console draws as counted, and a
// selection still copies what was written.
func TestConhostFrameDrawsEveryRuneAndCopiesTheOriginal(t *testing.T) {
	m, _ := testModel(t)
	m.glyphs = conhostTableFit()
	const text = "◆ ✅ 完成 🎉 ⚠️ 警告 👨\u200d👩\u200d👧 🇨🇳 1\ufe0f\u20e3 ☕"
	apply(m, eventwire.Event{Kind: "notice", Level: "info", Text: text})
	frame := ansi.Strip(m.View().Content)
	for i, row := range strings.Split(frame, "\n") {
		if got := conhostRowCells(row); got > m.width {
			t.Fatalf("row %d draws %d cells on conhost, terminal has %d:\n%s", i, got, m.width, row)
		}
		for _, r := range row {
			if r >= 0x80 && !linkedTestScript(r) && !conhostDraws(r) {
				t.Fatalf("the frame carries %U, which the console cannot draw whole:\n%s", r, frame)
			}
		}
	}
	joined := strings.Join(m.content(nil), "\n")
	row := strings.Count(joined[:strings.Index(joined, "◆")], "\n") - m.scr.yoff
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: row})
	m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: 70, Y: row})
	if got := m.selectedText(); !strings.Contains(got, text) {
		t.Fatalf("selected %q, want the original %q", got, text)
	}
}

func conhostDraws(r rune) bool {
	return conhostHasGlyph(r) && conhostCellsOf(r) == ansi.WcWidth.StringWidth(string(r))
}

// A symbol the console draws wider than counted and has no halfwidth form
// for is drawn a stand-in of its count, never a blank or the unmapped '?'.
func TestConhostSymbolsKeepTheirCount(t *testing.T) {
	g := conhostTableFit()
	for in, want := range map[string]string{
		"2 × 3 ± 1°": "2 x 3 + 1o",
		"α β Ω ※":    "a b W *",
		"↑/↓ 选择 ◆ x": "^/v 选择 * x",
	} {
		got := g.apply(in)
		if got != want {
			t.Fatalf("apply(%q) = %q, want %q", in, got, want)
		}
		if a, b := ansi.WcWidth.StringWidth(in), ansi.WcWidth.StringWidth(got); a != b {
			t.Fatalf("apply(%q) changed the per-rune count %d -> %d", in, a, b)
		}
	}
}
func linkedTestScript(r rune) bool {
	return r >= 0x4e00 && r <= 0x9fff || r >= 0x3000 && r <= 0x303f || r >= 0xff00 && r <= 0xffef
}

// A terminal drawing clusters gets letters for flags and the base character
// for keycaps and tag sequences, each at the cluster's own count; every other
// emoji reaches it as written.
func TestUnfontedClustersKeepTheGraphemeCount(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"🇨🇳 中国", "CN 中国"},
		{"1\ufe0f\u20e3 一", "1  一"},
		{"🏴\U000e0067\U000e0062\U000e0073\U000e0063\U000e0074\U000e007f", "🏴"},
		{"👨\u200d👩\u200d👧 ⚠\ufe0f 👍🏽", "👨\u200d👩\u200d👧 ⚠\ufe0f 👍🏽"},
		{"\x1b[1m🇯🇵\x1b[m", "\x1b[1mJP\x1b[m"},
	} {
		got := unfontedClusters(c.in)
		if got != c.want {
			t.Fatalf("unfontedClusters(%q) = %q, want %q", c.in, got, c.want)
		}
		if a, b := ansi.GraphemeWidth.StringWidth(c.in), ansi.GraphemeWidth.StringWidth(got); a != b {
			t.Fatalf("unfontedClusters(%q) changed the grapheme count %d -> %d", c.in, a, b)
		}
	}
}
