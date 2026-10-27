package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

type rootTriageTool struct {
	readProbe
	triage *Agent
	calls  int
	source string
}

func (p *rootTriageTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	p.calls++
	_, _ = p.triage.askTriage(ctx, "classify fixture", "fixture content", p.source)
	return "classification attempted", nil
}

func TestRootTriageTokenBudget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		source     string
		failure    bool
		missing    bool
		noEmission bool
		promptOnly bool
		disabled   bool
		wantCalls  int
	}{
		{name: "classifier", source: event.UsageSourceClassifier, wantCalls: 1},
		{name: "screen", source: event.UsageSourceInjectionScreen, wantCalls: 1},
		{name: "billed_failure", source: event.UsageSourceClassifier, failure: true, wantCalls: 1},
		{name: "missing", source: event.UsageSourceClassifier, missing: true, wantCalls: 2},
		{name: "disabled", source: event.UsageSourceClassifier, disabled: true, wantCalls: 4},
		{name: "no_sink", source: event.UsageSourceClassifier, noEmission: true, wantCalls: 1},
		{name: "counts_without_total", source: event.UsageSourceClassifier, promptOnly: true, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := billed(1000, 100)
			if tc.missing {
				u = nil
			}
			if tc.promptOnly {
				u.TotalTokens = 0
			}
			var fail error
			if tc.failure {
				fail = errors.New("billed classification failed")
			}
			replies := make([]scriptedReply, 4)
			for i := range replies {
				replies[i] = scriptedReply{text: "READONLY", usage: u, fail: fail}
			}
			sink := &recordSink{}
			triage := New(&mockProvider{}, tool.NewRegistry(), sessionstore.NewSession("triage"), Options{TriageProvider: &scriptedSummarizer{replies: replies}}, sink)
			if tc.noEmission {
				triage.svc.sink = nil
			}
			probe := &rootTriageTool{triage: triage, source: tc.source}
			reg := tool.NewRegistry()
			reg.Add(probe)
			p := &spendingProvider{max: 4}
			root := New(p, reg, sessionstore.NewSession("root"), Options{TaskBudget: TaskBudget{Tokens: 1500}}, sink)
			ctx := t.Context()
			if tc.disabled {
				ctx = WithTaskBudget(ctx, TaskBudget{})
			}
			err := root.Run(ctx, "classify fixture")
			if tc.disabled {
				if err != nil {
					t.Fatal(err)
				}
			} else if pause, ok := InspectRunPause(err); !ok || pause.Key != "token" {
				t.Fatalf("pause=%+v err=%v", pause, err)
			}
			if probe.calls != tc.wantCalls || p.rounds.Load() != int32(tc.wantCalls+1) {
				t.Fatalf("triage/normal=%d/%d want%d/%d", probe.calls, p.rounds.Load(), tc.wantCalls, tc.wantCalls+1)
			}
			var auxiliary int
			for _, e := range sink.kinds(event.Usage) {
				if e.Kind == event.Usage && e.UsageSource == tc.source {
					auxiliary += e.Usage.PromptTokens + e.Usage.CompletionTokens
				}
			}
			want := int(p.rounds.Load()) * 1100
			charged := 0
			if !tc.missing {
				charged = probe.calls * 1100
			}
			if root.task.budget.promptTokens+root.task.budget.outputTokens != want || root.task.budget.rootTokens.used.Load() != uint64(want+charged) {
				t.Fatalf("local/root=%d/%d want%d/%d", root.task.budget.promptTokens+root.task.budget.outputTokens, root.task.budget.rootTokens.used.Load(), want, want+charged)
			}
			if !tc.missing && !tc.noEmission && !tc.promptOnly && auxiliary != probe.calls*1100 {
				t.Fatal("triage fixture failed to bill exact usage")
			}
			if (tc.missing || tc.noEmission || tc.promptOnly) && auxiliary != 0 {
				t.Fatal("existing usage emission conditions changed")
			}
		})
	}
}

func TestRootTriageOutsideRunDoesNotCreateScope(t *testing.T) {
	a := New(&mockProvider{}, tool.NewRegistry(), sessionstore.NewSession("triage"), Options{TriageProvider: &scriptedSummarizer{replies: []scriptedReply{{text: "READONLY", usage: billed(1000, 100)}}}}, event.Discard)
	if text, ok := a.askTriage(t.Context(), "classify", "fixture", event.UsageSourceClassifier); !ok || text != "READONLY" {
		t.Fatalf("reply=%q ok=%v", text, ok)
	}
	if a.task.budget.rootTokens != nil {
		t.Fatal("out-of-run classification created an unrelated root")
	}
}
