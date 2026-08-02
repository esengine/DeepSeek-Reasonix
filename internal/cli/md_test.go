package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// TestRenderEmpty covers the contract that empty / whitespace-only input
// returns "" — callers rely on this to skip a redraw when there's nothing
// substantive to show.
func TestRenderEmpty(t *testing.T) {
	r := newMarkdownRenderer(80)
	for _, in := range []string{"", " ", "\n", "\t\n  \n"} {
		if got := r.Render(in); got != "" {
			t.Errorf("Render(%q) = %q, want empty", in, got)
		}
	}
}

// TestRenderConstructsRound-trip checks each major construct emits something
// styled while preserving the underlying text. We don't assert exact ANSI
// sequences (palette could shift) — only that key visible text survives and
// that we don't degrade to literal markdown.
func TestRenderConstructs(t *testing.T) {
	r := newMarkdownRenderer(80)
	cases := []struct {
		name     string
		in       string
		contains []string
		notRaw   []string // substrings that must NOT appear (raw markdown leaking through)
	}{
		{
			name:     "heading",
			in:       "# Hello\n",
			contains: []string{"Hello"},
			notRaw:   []string{"# Hello", "## "},
		},
		{
			name:     "heading h2 drops prefix",
			in:       "## Section\n",
			contains: []string{"Section"},
			notRaw:   []string{"## ", "###"},
		},
		{
			name:     "bold",
			in:       "this is **important** text",
			contains: []string{"important", "this is", "text"},
		},
		{
			name:     "italic",
			in:       "see *here* for details",
			contains: []string{"here", "see", "for details"},
		},
		{
			name:     "code span",
			in:       "use `os.Setenv` to set",
			contains: []string{"os.Setenv", "use", "to set"},
		},
		{
			name:     "unordered list",
			in:       "- one\n- two\n- three\n",
			contains: []string{"one", "two", "three", "•"},
		},
		{
			name:     "ordered list",
			in:       "1. first\n2. second\n",
			contains: []string{"first", "second", "1.", "2."},
		},
		{
			name:     "fenced code",
			in:       "```go\nfunc main() {}\n```\n",
			contains: []string{"func main()"},
		},
		{
			name:     "thematic break",
			in:       "above\n\n---\n\nbelow",
			contains: []string{"above", "below", "─"},
		},
		{
			name:     "gfm table",
			in:       "| name | size |\n|------|------|\n| a    | 12   |\n| bb   | 345  |\n",
			contains: []string{"name", "size", "a", "12", "bb", "345", "│"},
			notRaw:   []string{"|------|"}, // raw separator must be transformed
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := r.Render(tc.in)
			for _, want := range tc.contains {
				if !strings.Contains(out, want) {
					t.Errorf("Render(%q) missing %q\n--- output ---\n%s", tc.in, want, out)
				}
			}
			for _, leak := range tc.notRaw {
				if strings.Contains(out, leak) {
					t.Errorf("Render(%q) leaked raw markdown %q", tc.in, leak)
				}
			}
		})
	}
}

// TestRenderHeadingLevelColours pins the DeepCode v2 heading hierarchy: h1/h2
// render bold + accent, h3 and below bold with the default foreground.
func TestRenderHeadingLevelColours(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256
	configureCLITheme("dark")

	r := newMarkdownRenderer(80)
	out := r.Render("# Top\n\n## Mid\n\n### Deep\n\n#### Deeper\n")
	accentSGR := fgSGR(activeCLITheme.accent)
	lines := strings.Split(out, "\n")
	lineFor := func(text string) string {
		t.Helper()
		for _, ln := range lines {
			if strings.Contains(ansi.Strip(ln), text) {
				return ln
			}
		}
		t.Fatalf("rendered output lost the %q heading:\n%s", text, out)
		return ""
	}
	for _, text := range []string{"Top", "Mid"} {
		ln := lineFor(text)
		if !strings.Contains(ln, ansiBold) || !strings.Contains(ln, accentSGR) {
			t.Errorf("h1/h2 heading %q should be bold + accent: %q", text, ln)
		}
	}
	for _, text := range []string{"Deep", "Deeper"} {
		ln := lineFor(text)
		if !strings.Contains(ln, ansiBold) {
			t.Errorf("h3+ heading %q should stay bold: %q", text, ln)
		}
		if strings.Contains(ln, accentSGR) {
			t.Errorf("h3+ heading %q should drop the accent colour: %q", text, ln)
		}
	}
}

// TestRenderThematicBreakBorderColour pins the full-width rule in the theme
// border colour (DeepCode v2) instead of the old faint dim, and the plain
// NO_COLOR fallback.
func TestRenderThematicBreakBorderColour(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.ANSI256
	configureCLITheme("dark")

	r := newMarkdownRenderer(40)
	rule := strings.Repeat("─", 40)
	out := r.Render("above\n\n---\n\nbelow")
	if !strings.Contains(out, fgSGR(activeCLITheme.border)+rule) {
		t.Errorf("thematic break should be a full-width rule in the border colour:\n%q", out)
	}
	if strings.Contains(out, fgSGR(activeCLITheme.faint)+rule) {
		t.Errorf("thematic break still uses the old faint colour:\n%q", out)
	}

	activeColorProfile = colorprofile.NoTTY
	out = r.Render("above\n\n---\n\nbelow")
	if !strings.Contains(out, rule) {
		t.Errorf("NO_COLOR thematic break should keep the plain rule:\n%q", out)
	}
	if strings.Contains(out, "\033[") {
		t.Errorf("NO_COLOR thematic break should carry no escape codes:\n%q", out)
	}
}

// TestWrapAnsiCJK proves the wrap counter treats CJK as 2 cols, so a line of
// Chinese characters wraps at half the column count.
func TestWrapAnsiCJK(t *testing.T) {
	// Width 10 = room for 5 Chinese characters per row.
	in := strings.Repeat("中", 8)
	out := wrapAnsi(in, 10)
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrap, got 1 line: %q", out)
	}
	if visibleWidth(lines[0]) > 10 {
		t.Errorf("first line exceeds width: %d > 10", visibleWidth(lines[0]))
	}
}
