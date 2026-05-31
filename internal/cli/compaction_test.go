package cli

import (
	"strings"
	"testing"

	"reasonix/internal/event"
)

// TestCompactionCardLines locks the finished-compaction card: a header naming
// the count and trigger, the summary under a gutter, and an archive line.
func TestCompactionCardLines(t *testing.T) {
	lines := compactionCardLines(event.Compaction{
		Trigger:  "auto",
		Messages: 12,
		Summary:  "## Goal\n- do X\n## Files & code\n- a.go edited",
		Archive:  "/tmp/arch/20260531.jsonl",
	})

	joined := strings.Join(lines, "\n")
	if !strings.Contains(lines[0], "◆") {
		t.Errorf("header should carry the card glyph, got %q", lines[0])
	}
	for _, want := range []string{"Context compacted", "12", "auto"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("header missing %q: %q", want, lines[0])
		}
	}
	// Every summary line (and the archive line) sits under the "│" gutter.
	for _, want := range []string{"│ ## Goal", "│ - do X", "│ - a.go edited", "│ archived /tmp/arch/20260531.jsonl"} {
		if !strings.Contains(joined, want) {
			t.Errorf("card missing gutter line %q in:\n%s", want, joined)
		}
	}
}

// TestCompactionCardLinesNoArchive omits the archive line when none was written.
func TestCompactionCardLinesNoArchive(t *testing.T) {
	lines := compactionCardLines(event.Compaction{Trigger: "manual", Messages: 3, Summary: "- brief"})
	if strings.Contains(strings.Join(lines, "\n"), "archived") {
		t.Errorf("no archive path should mean no archive line: %v", lines)
	}
	if !strings.Contains(lines[0], "manual") {
		t.Errorf("header should reflect the manual trigger: %q", lines[0])
	}
}
