package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/event"
)

// TestDiffTabExpansion proves tab-indented code (Go) renders with no literal tabs
// and a bar that runs exactly to width — a tab has zero StringWidth but the
// terminal expands it, which would otherwise overflow the background bar.
func TestDiffTabExpansion(t *testing.T) {
	defer func(prev bool) { colorEnabled = prev }(colorEnabled)
	colorEnabled = true
	width := 40
	d := event.FileDiff{Diff: "@@ -1 +1 @@\n+\t\tresult := compute()\n", Added: 1}
	for _, r := range diffBody(d, "x.go", width, 40) {
		if strings.ContainsRune(r, '\t') {
			t.Errorf("row keeps a literal tab (terminal overflows the bar): %q", r)
		}
		if strings.Contains(r, "result") && ansi.StringWidth(r) != width {
			t.Errorf("bar width = %d, want %d: %q", ansi.StringWidth(r), width, r)
		}
	}
}

// TestRenderNarrowNoPanic guards the width math against tiny terminals.
func TestRenderNarrowNoPanic(t *testing.T) {
	defer func(prev bool) { colorEnabled = prev }(colorEnabled)
	colorEnabled = true
	d := event.FileDiff{Diff: "@@ -1 +1 @@\n-\told 你好\n+\tnew 世界\n", Added: 1, Removed: 1}
	for _, w := range []int{1, 2, 3, 5, 8, 20} {
		_ = diffBody(d, "x.go", w, 40)
		_ = toolCard("bash", `{"command":"go test ./... 你好 long command"}`, w)
	}
}

// TestDiffKeepsHeaderLikeContent proves a deleted "-- x" / added "++ y" line
// (which render as "--- x" / "+++ y") is kept, not mistaken for a file header.
func TestDiffKeepsHeaderLikeContent(t *testing.T) {
	d := event.FileDiff{
		Diff:    "--- a/q.sql\n+++ b/q.sql\n@@ -1,2 +1,2 @@\n-- old comment\n++ new tally\n",
		Added:   1,
		Removed: 1,
	}
	joined := strings.Join(diffBody(d, "q.sql", 80, 40), "\n")
	if !strings.Contains(joined, "old comment") || !strings.Contains(joined, "new tally") {
		t.Fatalf("header-like content was dropped:\n%s", joined)
	}
}
