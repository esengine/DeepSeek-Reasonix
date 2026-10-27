package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/model/boundedllm"
	"reasonix/internal/state/sessionstore"
)

type boundedBudgetProvider struct {
	calls         int
	missing, fail bool
}

func (*boundedBudgetProvider) Name() string { return "bounded-budget" }

func (p *boundedBudgetProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	ch := make(chan provider.Chunk, 3)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "reviewed"}
	if !p.missing {
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100}}
	}
	if p.fail {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: errors.New("billed review failure")}
	}
	close(ch)
	return ch, nil
}

type boundedBudgetTool struct {
	readProbe
	prov                  *boundedBudgetProvider
	noEmission, projected bool
	capture               context.Context
	failures              int
}

func (p *boundedBudgetTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	p.capture = InheritRootTokenBudget(context.Background(), ctx)
	if p.projected {
		ctx = p.capture
	}
	cfg := boundedllm.Config{Provider: p.prov, Sink: event.Discard, UsageSource: event.UsageSourceAdvisor}
	if p.noEmission {
		cfg.Sink = nil
		cfg.UsageSource = ""
	}
	answer, err := boundedllm.Call(ctx, cfg, "review policy", "bounded evidence")
	if err != nil {
		p.failures++
		return "review failed", nil
	}
	return answer, nil
}

func TestRootBoundedReviewerBudget(t *testing.T) {
	for _, tc := range []struct {
		name                                           string
		missing, fail, disabled, noEmission, projected bool
		wantCalls                                      int32
	}{
		{name: "reported", wantCalls: 2},
		{name: "billed_failure", fail: true, wantCalls: 2},
		{name: "missing_usage", missing: true, wantCalls: 3},
		{name: "disabled", disabled: true, wantCalls: 5},
		{name: "no_emission", noEmission: true, wantCalls: 2},
		{name: "projected_job_context", projected: true, wantCalls: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			aux := &boundedBudgetProvider{missing: tc.missing, fail: tc.fail}
			probe := &boundedBudgetTool{prov: aux, noEmission: tc.noEmission, projected: tc.projected}
			reg := tool.NewRegistry()
			reg.Add(probe)
			main := &spendingProvider{max: 4}
			root := New(main, reg, sessionstore.NewSession("root"), Options{TaskBudget: TaskBudget{Tokens: 1500}}, event.Discard)
			ctx := t.Context()
			if tc.disabled {
				ctx = WithTaskBudget(ctx, TaskBudget{})
			}
			err := root.Run(reportedBudgetContext(ctx), "obtain a review")
			if tc.disabled {
				if err != nil {
					t.Fatal(err)
				}
			} else if pause, ok := InspectRunPause(err); !ok || pause.Key != "token" {
				t.Fatalf("pause=%+v err=%v", pause, err)
			}
			if main.rounds.Load() != tc.wantCalls || aux.calls != int(tc.wantCalls-1) {
				t.Fatalf("normal/reviewer calls=%d/%d, want%d/%d", main.rounds.Load(), aux.calls, tc.wantCalls, tc.wantCalls-1)
			}
			want := uint64(main.rounds.Load()) * 1100
			if !tc.missing {
				want += uint64(aux.calls) * 1100
			}
			if root.task.budget.rootTokens.used.Load() != want {
				t.Fatalf("root tokens=%d,want%d", root.task.budget.rootTokens.used.Load(), want)
			}
			if root.task.budget.promptTokens+root.task.budget.outputTokens != int(main.rounds.Load())*1100 {
				t.Fatal("bounded usage changed local normal telemetry")
			}
			if tc.fail && probe.failures != aux.calls {
				t.Fatal("fixture did not retain each billed failure")
			}
		})
	}
}

func TestRootBoundedNestedBudgetChargesOnce(t *testing.T) {
	aux := &boundedBudgetProvider{}
	childTools := tool.NewRegistry()
	childTools.Add(&boundedBudgetTool{prov: aux})
	childProv := &spendingProvider{max: 4}
	child := New(childProv, childTools, sessionstore.NewSession("child"), Options{}, event.Discard)
	parentTools := tool.NewRegistry()
	parentTools.Add(&tokenBudgetChildTool{child: child})
	parentProv := &spendingProvider{max: 1}
	root := New(parentProv, parentTools, sessionstore.NewSession("root"), Options{TaskBudget: TaskBudget{Tokens: 2500}}, event.Discard)
	err := root.Run(reportedBudgetContext(t.Context()), "delegate the review")
	if pause, ok := InspectRunPause(err); !ok || pause.Key != "token" {
		t.Fatalf("pause=%+v err=%v", pause, err)
	}
	if parentProv.rounds.Load() != 2 || childProv.rounds.Load() != 2 || aux.calls != 1 {
		t.Fatalf("parent/child/aux=%d/%d/%d,want2/2/1", parentProv.rounds.Load(), childProv.rounds.Load(), aux.calls)
	}
	if got := root.task.budget.rootTokens.used.Load(); got != 5500 {
		t.Fatalf("root tokens=%d,want5500 without duplicate observers", got)
	}
}

func TestRootBoundedLateProjectionStaysWithOldTask(t *testing.T) {
	aux := &boundedBudgetProvider{}
	probe := &boundedBudgetTool{prov: aux}
	reg := tool.NewRegistry()
	reg.Add(probe)
	root := New(&spendingProvider{max: 1}, reg, sessionstore.NewSession("root"), Options{}, event.Discard)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	if err := root.Run(reportedBudgetContext(ctx), "capture budget"); err != nil {
		t.Fatal(err)
	}
	old := root.task.budget.rootTokens
	before := old.used.Load()
	cancel()
	if err := root.Run(reportedBudgetContext(t.Context()), "new task"); err != nil {
		t.Fatal(err)
	}
	current := root.task.budget.rootTokens
	currentBefore := current.used.Load()
	if old == current {
		t.Fatal("new task kept old scope")
	}
	if _, err := boundedllm.Call(probe.capture, boundedllm.Config{Provider: aux}, "review policy", "late evidence"); err != nil {
		t.Fatal(err)
	}
	if old.used.Load() != before+1100 || current.used.Load() != currentBefore {
		t.Fatalf("late usage went to wrong scope:old%d/%d,new%d/%d", old.used.Load(), before, current.used.Load(), currentBefore)
	}
}
