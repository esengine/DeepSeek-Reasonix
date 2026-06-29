package memorycompiler

import (
	"context"
	"strings"
	"testing"
)

// #1: a turn that finishes without error and without tool calls is a success
// (a plain-text answer), not partial_success, and must not be learned as a
// strategy failure. The earlier behavior demoted every no-tool turn and, via
// updateStrategy, recorded it as a failure — poisoning scores with Memory v5 on.
func TestNoToolSuccessfulTurnIsSuccess(t *testing.T) {
	if got := outcomeFor(nil, nil); got != "success" {
		t.Fatalf("outcomeFor(no tools, no err) = %q, want success", got)
	}
	dir := t.TempDir()
	rt := New(dir)
	for i := 0; i < 5; i++ {
		_, turn := rt.StartTurn(context.Background(), "explain how the auth module works", nil)
		if turn == nil {
			t.Fatalf("turn %d nil", i)
		}
		turn.Finish(nil) // no RecordToolResults => zero tool records, err == nil
	}
	st := rt.loadState()
	for _, s := range st.Strategies {
		if s.Failures > 0 {
			t.Errorf("strategy %q got %d failures from no-tool successful turns", s.ID, s.Failures)
		}
	}
}

// #2: goal classification must strip the injected "Referenced context:" preamble
// and file blocks, while SourceEvent keeps the full input (the model's only view
// of the referenced files once the contract replaces the user turn).
func TestStripReferencedContextForGoalOnly(t *testing.T) {
	raw := "Referenced context:\n\n<file path=\"a.go\">package main\nfunc secret() {}\n</file>\n\nfix the flaky login test"
	if got := stripReferencedContext(raw); got != "fix the flaky login test" {
		t.Fatalf("stripReferencedContext = %q, want the user's actual text", got)
	}
	if got := stripReferencedContext("just a normal goal"); got != "just a normal goal" {
		t.Fatalf("plain input changed: %q", got)
	}

	dir := t.TempDir()
	rt := New(dir)
	_, turn := rt.StartTurn(context.Background(), raw, nil)
	if turn == nil {
		t.Fatal("nil turn")
	}
	if strings.Contains(turn.ir.Goal, "secret") || strings.Contains(turn.ir.Goal, "package main") {
		t.Fatalf("goal leaked file contents: %q", turn.ir.Goal)
	}
	if !strings.Contains(turn.ir.SourceEvent, "secret") {
		t.Fatalf("source_event dropped the referenced file the model needs: %q", turn.ir.SourceEvent)
	}
}
