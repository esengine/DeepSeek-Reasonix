package memorycompiler

import (
	"context"
	"testing"
)

func seedRepeatedFailure(t *testing.T, rt *Runtime, turns int, errText string) {
	t.Helper()
	for i := 0; i < turns; i++ {
		_, turn := rt.StartTurn(context.Background(), "fix a bug", nil)
		turn.RecordToolResults([]ToolRecord{
			{Name: "bash", Error: errText},
			{Name: "bash", Error: errText},
		})
		turn.Finish(nil)
	}
}

func TestStableNoisePatternsRequireMinCount(t *testing.T) {
	rt := New(t.TempDir())
	seedRepeatedFailure(t, rt, 1, "cannot find module providing package foo")

	if got := rt.StableNoisePatterns(2, 0); len(got) != 0 {
		t.Fatalf("single-turn pattern should not be stable yet: %+v", got)
	}

	seedRepeatedFailure(t, rt, 1, "cannot find module providing package foo")
	got := rt.StableNoisePatterns(2, 0)
	if len(got) != 1 {
		t.Fatalf("patterns = %+v, want exactly one stable pattern", got)
	}
	if got[0].Count != 2 || got[0].Pattern != "bash repeated error: cannot find module providing package foo" {
		t.Fatalf("unexpected stable pattern: %+v", got[0])
	}
}

func TestStableNoisePatternsNilAndEmpty(t *testing.T) {
	var nilRT *Runtime
	if got := nilRT.StableNoisePatterns(1, 0); got != nil {
		t.Fatalf("nil runtime returned patterns: %+v", got)
	}
	if got := New(t.TempDir()).StableNoisePatterns(1, 0); len(got) != 0 {
		t.Fatalf("fresh dir returned patterns: %+v", got)
	}
}

func TestStableNoisePatternsOrderedAndBounded(t *testing.T) {
	rt := New(t.TempDir())
	seedRepeatedFailure(t, rt, 3, "error alpha")
	seedRepeatedFailure(t, rt, 2, "error beta")

	got := rt.StableNoisePatterns(2, 1)
	if len(got) != 1 {
		t.Fatalf("patterns = %+v, want maxItems to bound the list", got)
	}
	if got[0].Count != 3 {
		t.Fatalf("expected the most frequent pattern first: %+v", got[0])
	}
}
