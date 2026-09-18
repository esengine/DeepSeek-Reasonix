package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// strippedRows renders thinking text and returns the visible (ANSI-free) rows.
func strippedRows(raw string, width int) []string {
	rows := reasoningRows(raw, width, 0, false)
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = ansi.Strip(row)
	}
	return out
}

// TestReasoningRowsRespectWindowBoundary proves no rendered row overruns the
// window after its own indent (the connector on the first line, the paragraph
// indent on later first lines, the smaller wrap indent on continuations).
func TestReasoningRowsRespectWindowBoundary(t *testing.T) {
	const width = 60
	raw := strings.Repeat("word ", 80) + "\n\n" + strings.Repeat("token ", 40)
	for i, row := range reasoningRows(raw, width, 0, false) {
		if w := ansi.StringWidth(row); w > width {
			t.Fatalf("row %d width %d exceeds window %d: %q", i, w, width, ansi.Strip(row))
		}
	}
}

// TestReasoningRowsParagraphIndent proves the block's first line carries the "⎿"
// connector, a later paragraph re-indents its first line to the same column, and
// wrapped overflow sits two columns left of it. The blank line between two
// prose paragraphs is dropped.
func TestReasoningRowsParagraphIndent(t *testing.T) {
	rows := strippedRows("alpha one\n\nbeta two", 80)
	want := []string{"  ⎿  alpha one", "     beta two"}
	if len(rows) != len(want) {
		t.Fatalf("rows = %q, want %q", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("row %d = %q, want %q (all: %q)", i, rows[i], want[i], rows)
		}
	}
}

// TestReasoningRowsListItemsAlign proves each source line starts at the paragraph
// indent, so numbered/bulleted list items align instead of reading as wrapped
// continuations of the line above.
func TestReasoningRowsListItemsAlign(t *testing.T) {
	raw := "Thinking about it.\n\nSo plan:\n1. first\n2. second\n3. third"
	got := strippedRows(raw, 80)
	want := []string{
		"  ⎿  Thinking about it.",
		"     So plan:",
		"     1. first",
		"     2. second",
		"     3. third",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("list rows:\n got %q\nwant %q", got, want)
	}
}

// TestReasoningRowsContinuationIndent proves soft-wrapped overflow uses the
// 3-column wrap indent rather than the 5-column line indent, and that it uses
// the wider continuation budget (a full row reaches the window edge).
func TestReasoningRowsContinuationIndent(t *testing.T) {
	const width = 40
	rows := strippedRows(strings.Repeat("word ", 40), width)
	if len(rows) < 2 {
		t.Fatalf("expected a wrapped line, got %q", rows)
	}
	if !strings.HasPrefix(rows[0], "  ⎿  ") {
		t.Fatalf("first row should carry the connector, got %q", rows[0])
	}
	for i, row := range rows[1:] {
		if !strings.HasPrefix(row, "   ") || strings.HasPrefix(row, "    ") {
			t.Fatalf("overflow row %d should be indented 3 spaces, got %q", i+1, row)
		}
		if w := visibleWidth(row); w > width {
			t.Fatalf("overflow row %d width %d exceeds window %d: %q", i+1, w, width, row)
		}
	}
}

// TestReasoningRowsPreserveFenceBlanks proves blank lines are kept inside a
// fenced code block (where they carry meaning) while the blanks around the fence
// and between prose paragraphs collapse.
func TestReasoningRowsPreserveFenceBlanks(t *testing.T) {
	raw := strings.Join([]string{
		"intro",
		"",
		"```go",
		"line one",
		"",
		"line two",
		"```",
		"",
		"outro",
	}, "\n")
	got := strippedRows(raw, 80)
	want := []string{
		"  ⎿  intro",
		"     ```go",
		"     line one",
		"",
		"     line two",
		"     ```",
		"     outro",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("fence blanks:\n got %q\nwant %q", got, want)
	}
}

// TestReasoningBlockCopyMatchesVisible proves the copy rendition strips to the
// same visible text as the display rendition (the gutters are copy-omitted).
func TestReasoningBlockCopyMatchesVisible(t *testing.T) {
	defer restoreThemeForTest(activeColorProfile, activeCLITheme)
	activeColorProfile = colorprofile.NoTTY
	configureCLITheme("dark")

	raw := "first\n\n```\ncode\n```\n\nsecond"
	visible := ansi.Strip(reasoningBlock(raw, 80, 0))
	copied := ansi.Strip(reasoningBlockCopy(raw, 80, 0))
	if visible != copied {
		t.Fatalf("copy rendition visible text = %q, want %q", copied, visible)
	}
}
