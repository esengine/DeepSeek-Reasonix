package cli

import (
	"strings"
	"testing"
)

// TestClampWidth guards the inline-overflow fix: scrollback lines wider than the
// viewport get hard-broken (so the renderer's scroll estimate stays exact), while
// lines within width — including space-padded table rows — are left untouched.
func TestClampWidth(t *testing.T) {
	// Within width: byte-for-byte identical (runs of spaces must NOT collapse).
	row := "│ a    │ bb │"
	if got := clampWidth(row, 80); got != row {
		t.Errorf("within-width line altered: %q -> %q", row, got)
	}
	// Over width: every resulting line fits, content is preserved.
	long := strings.Repeat("x", 200)
	out := clampWidth(long, 40)
	for _, line := range strings.Split(out, "\n") {
		if visibleWidth(line) > 40 {
			t.Errorf("clamped line exceeds 40: width=%d", visibleWidth(line))
		}
	}
	if strings.ReplaceAll(out, "\n", "") != long {
		t.Error("clampWidth lost or altered content")
	}
	// width <= 0 is a no-op (pre-sizing).
	if clampWidth(long, 0) != long {
		t.Error("width<=0 should be a no-op")
	}
}

// TestCommitReasoningWrapsToWidth guards the ghost-border fix: every line a
// reasoning commit queues for scrollback must fit the viewport width, so
// bubbletea's Println erases each line to its end and the old input-box border
// can't bleed through after a wrapped reasoning row.
func TestCommitReasoningWrapsToWidth(t *testing.T) {
	const width = 40
	commit := []string{}
	m := &chatTUI{
		width:         width,
		reasoning:     &strings.Builder{},
		pendingCommit: &commit,
	}
	// Header (short, indented) + a long single-line reasoning paragraph, both
	// dim-wrapped exactly as the agent streams them.
	m.reasoning.WriteString("\x1b[2m  ▎ thinking\x1b[0m\n")
	m.reasoning.WriteString("\x1b[2m" + strings.Repeat("reason ", 30) + "\x1b[0m")

	m.commitReasoning()

	if len(commit) == 0 {
		t.Fatal("commitReasoning queued nothing")
	}
	for _, block := range commit {
		for _, line := range strings.Split(block, "\n") {
			if w := visibleWidth(line); w > width {
				t.Errorf("committed line exceeds width %d: width=%d %q", width, w, line)
			}
		}
	}
	// The header keeps its leading indent (short lines pass through verbatim).
	if !strings.Contains(commit[0], "  ▎ thinking") {
		t.Errorf("header indent not preserved: %q", commit[0])
	}
	// Reasoning must be cleared after committing.
	if m.reasoning.Len() != 0 {
		t.Error("reasoning buffer not reset")
	}
}
