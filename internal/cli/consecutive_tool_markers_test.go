package cli

import (
	"strings"
	"testing"

	"reasonix/internal/event"
)

// 3 parallel Bash(ls) in one turn, ids "call_<n>" (no "shell-" prefix), each
// streams 22 lines then finishes. Every card must keep its own "⎿ 22 lines"
// marker. Regression: collapseShellSlot's late path recovered the count only
// from shellOutputs ("shell-" ids), so call_N ids fell through to "-1 lines".
func TestParallelBashMarkersKeepOwnLineCount(t *testing.T) {
	m := newTestChatTUI()
	ids := []string{"call_1", "call_2", "call_3"}
	for _, id := range ids {
		m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: id, Name: "bash", Partial: true}})
	}
	for _, id := range ids {
		m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: id, Name: "bash", Args: `{"command":"ls"}`, Partial: false}})
	}
	for _, id := range ids {
		for i := 0; i < 22; i++ {
			m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: id, Output: "line\n"}})
		}
	}
	for _, id := range ids {
		m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: id, Name: "bash", Output: strings.Repeat("line\n", 22)}})
	}
	transcript := m.transcript
	if joined := strings.Join(transcript, "\n"); strings.Contains(joined, "-1 lines") {
		t.Fatalf("transcript must not contain a negative line count:\n%s", joined)
	}
	// Locate each card and assert the marker directly below it contains the
	// correct line count. With 22 lines of output per call the summary is
	// "⎿ 22 lines".
	cardIdx := map[string]int{}
	for i, ln := range transcript {
		if idx, ok := cardIdx["c1"]; !ok && strings.Contains(ln, "Bash(ls)") {
			_ = idx
			if _, seen := cardIdx["c1"]; !seen {
				cardIdx["c1"] = i
				continue
			}
		}
		if _, seen := cardIdx["c2"]; !seen && len(cardIdx) == 1 && strings.Contains(ln, "Bash(ls)") {
			cardIdx["c2"] = i
			continue
		}
		if _, seen := cardIdx["c3"]; !seen && len(cardIdx) == 2 && strings.Contains(ln, "Bash(ls)") {
			cardIdx["c3"] = i
		}
	}
	if len(cardIdx) != 3 {
		t.Fatalf("expected three bash cards in transcript, got %v\n%s", cardIdx, strings.Join(transcript, "\n"))
	}
	for name, idx := range cardIdx {
		marker := transcript[idx+1]
		if !strings.Contains(marker, "⎿") {
			t.Fatalf("%s: marker slot at transcript[%d] should contain ⎿, got %q\nfull transcript:\n%s",
				name, idx+1, marker, strings.Join(transcript, "\n"))
		}
		if !strings.Contains(marker, "22 lines") {
			t.Fatalf("%s: marker slot at transcript[%d] should report 22 lines, got %q\nfull transcript:\n%s",
				name, idx+1, marker, strings.Join(transcript, "\n"))
		}
	}
}

// No-streaming variant: a second Bash dispatches before the first emits any
// ToolProgress, and the first's result lands last. The slot must still show
// "⎿ N lines" driven by the ToolResult's own Output, not "-1 lines" or blank.
func TestNonShellToolLateResultShowsCorrectCount(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "call_a", Name: "bash", Args: `{"command":"echo a"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "call_b", Name: "bash", Args: `{"command":"echo b"}`}})
	// No ToolProgress for either; the result is the only signal.
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "call_a", Name: "bash", Output: "a\nsecond\nthird\n"}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "call_b", Name: "bash", Output: "b\n"}})
	transcript := m.transcript
	joined := strings.Join(transcript, "\n")
	if strings.Contains(joined, "-1 lines") {
		t.Fatalf("transcript must not contain a negative line count:\n%s", joined)
	}
	wantSubstrings := map[string]string{
		"call_a": "3 lines", // a\nsecond\nthird\n → 3 lines
		"call_b": "1 lines", // b\n → 1 line
	}
	cardIdx := map[string]int{}
	for i, ln := range transcript {
		if strings.Contains(ln, "echo a") {
			cardIdx["call_a"] = i
		}
		if strings.Contains(ln, "echo b") {
			cardIdx["call_b"] = i
		}
	}
	for id, want := range wantSubstrings {
		idx, ok := cardIdx[id]
		if !ok {
			t.Fatalf("missing %s card in transcript:\n%s", id, joined)
		}
		marker := transcript[idx+1]
		if !strings.Contains(marker, "⎿") {
			t.Fatalf("%s: marker at transcript[%d] should contain ⎿, got %q", id, idx+1, marker)
		}
		if !strings.Contains(marker, want) {
			t.Fatalf("%s: marker at transcript[%d] should report %s, got %q", id, idx+1, want, marker)
		}
	}
}
