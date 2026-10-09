package tui

import (
	"strings"
	"testing"
)

// Reasoning with no text in it has nothing to fold, so neither a thinking-only
// step nor an answer carries a "thought for 0s" marker.
func TestBlankThinkingDrawsNoMarker(t *testing.T) {
	for _, reasoning := range []string{"", "\n\n", " \t\n"} {
		step := Item{Kind: ItemSay, Reasoning: reasoning, Done: true}
		if got := renderItem(&step, 80, 0, false); got != "" {
			t.Errorf("thinking-only step %q drew %q", reasoning, got)
		}
		answer := Item{Kind: ItemSay, Text: "done", Reasoning: reasoning, Done: true}
		if got := renderItem(&answer, 80, 0, false); strings.Contains(got, "thought for") {
			t.Errorf("answer after blank thinking %q drew a marker:\n%s", reasoning, got)
		}
	}
	real := Item{Kind: ItemSay, Text: "done", Reasoning: "weighing it", Done: true, ThoughtMs: 3000}
	if got := renderItem(&real, 80, 0, false); !strings.Contains(got, "thought for 3s") {
		t.Fatalf("real thinking lost its marker:\n%s", got)
	}
}
