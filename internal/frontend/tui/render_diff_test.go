package tui

import (
	"strings"
	"testing"

	"reasonix/internal/contract/eventwire"
)

const tuiDiff = "diff --git a/x.go b/x.go\n" +
	"--- a/x.go\n" +
	"+++ b/x.go\n" +
	"@@ -1 +1 @@\n" +
	"-OLD_LINE\n" +
	"+NEW_LINE\n"

func bashDiffEvent(diff bool) []eventwire.Event {
	return []eventwire.Event{
		{Kind: "tool_dispatch", Tool: &eventwire.Tool{ID: "t1", Name: "bash", Args: `{"command":"git diff"}`}},
		{Kind: "tool_result", Tool: &eventwire.Tool{ID: "t1", Name: "bash", Output: tuiDiff, OutputDiff: diff}},
	}
}

// A shell result the host marked as a whole diff renders as a diff card — the
// file header with its +/- tally, and the changed rows — not the flat preview.
func TestMarkedShellDiffRendersAsDiffRows(t *testing.T) {
	m, _ := testModel(t)
	apply(m, bashDiffEvent(true)...)
	joined := strings.Join(screenRows(m), "\n")
	if !strings.Contains(joined, "x.go") || !strings.Contains(joined, "+1 -1") {
		t.Fatalf("marked diff lost its header tally:\n%s", joined)
	}
	if !strings.Contains(joined, "NEW_LINE") || !strings.Contains(joined, "OLD_LINE") {
		t.Fatalf("marked diff lost its body:\n%s", joined)
	}
}

// A marked shell diff keeps the "⎿" connector every other tool card's body
// uses, instead of dropping straight to the diff rows.
func TestMarkedShellDiffKeepsConnector(t *testing.T) {
	m, _ := testModel(t)
	apply(m, bashDiffEvent(true)...)
	joined := strings.Join(screenRows(m), "\n")
	if !strings.Contains(joined, "⎿") {
		t.Fatalf("marked diff lost the connector:\n%s", joined)
	}
}

// Without the marker the same result stays the plain shell preview, so the
// header tally never appears.
func TestUnmarkedShellDiffStaysProse(t *testing.T) {
	m, _ := testModel(t)
	apply(m, bashDiffEvent(false)...)
	joined := strings.Join(screenRows(m), "\n")
	if strings.Contains(joined, "+1 -1") {
		t.Fatalf("unmarked diff was rendered as a diff card:\n%s", joined)
	}
}
