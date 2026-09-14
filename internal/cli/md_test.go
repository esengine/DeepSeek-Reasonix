package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
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

// TestRenderDiffFence proves a ```diff fence renders through the colourised
// diff path (add/remove backgrounds) instead of the generic code rail.
func TestRenderDiffFence(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256

	r := newMarkdownRenderer(80)
	out := r.Render("```diff\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n```\n")
	if strings.Contains(out, "│ ") {
		t.Fatalf("diff fence should not use the code rail:\n%s", out)
	}
	for _, want := range []string{bgDiffAdd, bgDiffDel} {
		if !strings.Contains(out, want) {
			t.Fatalf("diff fence missing background %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "new") || !strings.Contains(out, "old") {
		t.Fatalf("diff fence dropped content:\n%s", out)
	}
	if !strings.Contains(out, "x.go") {
		t.Fatalf("diff fence header should name the file:\n%s", out)
	}
	if strings.Contains(out, "--- a/x.go") || strings.Contains(out, "+++ b/x.go") {
		t.Fatalf("diff fence should drop the raw file-header pair:\n%s", out)
	}
}

// TestRenderDiffFenceMultiFile proves a git-style fence with several files gets
// one path header per file, with the git preamble stripped from the rows.
func TestRenderDiffFenceMultiFile(t *testing.T) {
	defer func(prev colorprofile.Profile) { activeColorProfile = prev }(activeColorProfile)
	activeColorProfile = colorprofile.ANSI256

	r := newMarkdownRenderer(80)
	out := r.Render("```diff\n" +
		"diff --git a/one.go b/one.go\nindex 111..222 100644\n--- a/one.go\n+++ b/one.go\n@@ -1 +1 @@\n-old\n+new\n" +
		"diff --git a/two.go b/two.go\nindex 333..444 100644\n--- a/two.go\n+++ b/two.go\n@@ -1 +1 @@\n-gone\n+kept\n" +
		"```\n")
	if n := strings.Count(out, "one.go"); n != 1 {
		t.Fatalf("want one header naming one.go, got %d occurrences:\n%s", n, out)
	}
	if n := strings.Count(out, "two.go"); n != 1 {
		t.Fatalf("want one header naming two.go, got %d occurrences:\n%s", n, out)
	}
	for _, leak := range []string{"diff --git", "index 111", "index 333"} {
		if strings.Contains(out, leak) {
			t.Fatalf("git preamble %q leaked into the rows:\n%s", leak, out)
		}
	}
}
