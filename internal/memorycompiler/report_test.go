package memorycompiler

import (
	"context"
	"strings"
	"testing"
)

func TestLearningsReportEmpty(t *testing.T) {
	var nilRT *Runtime
	if _, ok := nilRT.LearningsReport(0); ok {
		t.Fatal("nil runtime reported learned state")
	}
	if _, ok := New(t.TempDir()).LearningsReport(0); ok {
		t.Fatal("fresh dir reported learned state")
	}
}

func TestLearningsReportAndFormat(t *testing.T) {
	dir := t.TempDir()
	rt := New(dir)
	_, fail := rt.StartTurn(context.Background(), "fix a bug", nil)
	fail.RecordToolResults([]ToolRecord{
		{Name: "bash", Error: "exit status 1"},
		{Name: "bash", Error: "exit status 1"},
	})
	fail.Finish(nil)
	_, ok := rt.StartTurn(context.Background(), "fix another bug", nil)
	ok.Finish(nil)

	rep, hasState := rt.LearningsReport(0)
	if !hasState {
		t.Fatal("expected learned state")
	}
	if len(rep.Strategies) == 0 {
		t.Fatalf("no strategies in report: %+v", rep)
	}
	if rep.Strategies[0].ID != "bugfix-reproduce-first" {
		t.Fatalf("expected bugfix strategy first, got %q", rep.Strategies[0].ID)
	}

	text := FormatLearningsReport(rep)
	for _, want := range []string{"memory-v5 learnings", "strategies:", "bugfix-reproduce-first", "ok / "} {
		if !strings.Contains(text, want) {
			t.Fatalf("formatted report missing %q:\n%s", want, text)
		}
	}
}
