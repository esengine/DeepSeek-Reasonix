package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

type rootMaintenanceTool struct {
	readProbe
	child     *Agent
	diagnosis bool
	calls     int
}

type rootAutomaticMaintenanceProvider struct {
	normal  spendingProvider
	summary scriptedSummarizer
}

func (p *rootAutomaticMaintenanceProvider) Name() string { return "root-automatic-maintenance" }

func (p *rootAutomaticMaintenanceProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if len(req.Tools) == 0 {
		return p.summary.Stream(ctx, req)
	}
	return p.normal.Stream(ctx, req)
}

func TestRootAuxiliaryAutomaticCompaction(t *testing.T) {
	p := &rootAutomaticMaintenanceProvider{normal: spendingProvider{max: 4}, summary: scriptedSummarizer{replies: []scriptedReply{{text: "## Standing facts\n- never change the public API", usage: billed(1000, 100)}}}}
	reg := tool.NewRegistry()
	reg.Add(readProbe{})
	a := New(p, reg, foldableSessionOverForce(30), Options{ContextWindow: 5000, CompactRatio: 0.5, RecentKeep: 2, ArchiveDir: t.TempDir(), TaskBudget: TaskBudget{Tokens: 1500}}, event.Discard)
	err := a.Run(t.Context(), "read a few files")
	if pause, ok := InspectRunPause(err); !ok || pause.Key != "token" {
		t.Fatalf("pause=%+v err=%v, want token landing", pause, err)
	}
	if p.summary.calls != 1 || p.normal.rounds.Load() != 2 {
		t.Fatalf("summary requests=%d normal requests=%d, want 1/2", p.summary.calls, p.normal.rounds.Load())
	}
	if got := a.task.budget.rootTokens.used.Load(); got != 3300 {
		t.Fatalf("root tokens=%d, want summary and two normal reported usages=3300", got)
	}
}

func (p *rootMaintenanceTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	p.calls++
	if p.diagnosis {
		code := 1
		region := []provider.Message{{Role: provider.RoleTool, Content: strings.Join(padded("failure line ", 40), "\n"), ToolExecution: &provider.ToolExecution{State: tool.ShellStateFailed, ExitCode: &code}}}
		p.child.annotateFailureDiagnostics(ctx, region, nil)
	} else {
		_, _ = p.child.CompactNow(ctx, CompactRequest{IgnoreEconomics: true})
	}
	return "maintenance attempted", nil
}

func TestRootAuxiliaryTokenBudget(t *testing.T) {
	for _, tc := range []struct {
		name      string
		diagnosis bool
		fail      bool
		missing   bool
		disabled  bool
		wantReads int
	}{
		{name: "summary", wantReads: 1},
		{name: "summary_failed", fail: true, wantReads: 1},
		{name: "summary_missing", missing: true, wantReads: 2},
		{name: "summary_disabled", disabled: true, wantReads: 4},
		{name: "diagnosis", diagnosis: true, wantReads: 1},
		{name: "diagnosis_failed", diagnosis: true, fail: true, wantReads: 1},
		{name: "diagnosis_missing", diagnosis: true, missing: true, wantReads: 2},
		{name: "diagnosis_disabled", diagnosis: true, disabled: true, wantReads: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := billed(1000, 100)
			if tc.missing {
				u = nil
			}
			var failure error
			if tc.fail {
				failure = errors.New("billed stream failed")
			}
			replies := make([]scriptedReply, 4)
			for i := range replies {
				replies[i] = scriptedReply{text: "## Standing facts\n- keep the public API", usage: u, fail: failure}
			}
			if tc.diagnosis {
				for i := range replies {
					replies[i].text = "1-3"
				}
			}
			sink := &recordSink{}
			child := New(&scriptedSummarizer{replies: replies}, tool.NewRegistry(), foldableSessionOverForce(30), Options{ContextWindow: 200000, RecentKeep: 2}, sink)
			probe := &rootMaintenanceTool{child: child, diagnosis: tc.diagnosis}
			reg := tool.NewRegistry()
			reg.Add(probe)
			prov := &spendingProvider{max: 4}
			root := New(prov, reg, sessionstore.NewSession("root"), Options{TaskBudget: TaskBudget{Tokens: 1500}}, sink)
			ctx := t.Context()
			if tc.disabled {
				ctx = WithTaskBudget(ctx, TaskBudget{})
			}
			err := root.Run(ctx, "run maintenance")
			if tc.disabled {
				if err != nil {
					t.Fatal(err)
				}
			} else if pause, ok := InspectRunPause(err); !ok || pause.Key != "token" {
				t.Fatalf("pause=%+v err=%v, want token landing", pause, err)
			}
			if probe.calls != tc.wantReads || prov.rounds.Load() != int32(tc.wantReads+1) {
				t.Fatalf("tool calls=%d provider rounds=%d, want %d/%d", probe.calls, prov.rounds.Load(), tc.wantReads, tc.wantReads+1)
			}
			_, input, output := ledgerTotal(sink)
			want := uint64(prov.rounds.Load())*1100 + uint64(input+output)
			if root.task.budget.promptTokens+root.task.budget.outputTokens != int(prov.rounds.Load())*1100 {
				t.Fatal("auxiliary usage changed local normal-round telemetry")
			}
			if got := root.task.budget.rootTokens.used.Load(); got != want {
				t.Fatalf("root tokens=%d, want normal plus auxiliary emitted usage=%d", got, want)
			}
			if !tc.missing && input+output == 0 {
				t.Fatal("fixture did not bill auxiliary usage")
			}
		})
	}
}

func TestRootAuxiliaryManualCompactionOutsideRun(t *testing.T) {
	sink := &recordSink{}
	a := New(&scriptedSummarizer{replies: []scriptedReply{{text: "## Standing facts\n- keep public APIs", usage: billed(1000, 100)}}}, tool.NewRegistry(), foldableSessionOverForce(30), Options{ContextWindow: 200000, RecentKeep: 2}, sink)
	if _, err := a.CompactNow(t.Context(), CompactRequest{IgnoreEconomics: true}); err != nil {
		t.Fatal(err)
	}
	if a.task.budget.rootTokens != nil {
		t.Fatal("manual maintenance created an unrelated root scope")
	}
	if calls, input, output := ledgerTotal(sink); calls != 1 || input+output != 1100 {
		t.Fatalf("maintenance bill=%d/%d/%d", calls, input, output)
	}
}

type rootMaintenanceCapture struct {
	readProbe
	projected context.Context
}

func (p *rootMaintenanceCapture) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	p.projected = InheritRootTokenBudget(context.Background(), ctx)
	return "captured budget scope", nil
}

func TestRootAuxiliaryBudgetProjectionKeepsOldTask(t *testing.T) {
	reg := tool.NewRegistry()
	capture := &rootMaintenanceCapture{}
	reg.Add(capture)
	root := New(&spendingProvider{max: 1}, reg, sessionstore.NewSession("root"), Options{}, event.Discard)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	if err := root.Run(ctx, "capture root scope"); err != nil {
		t.Fatal(err)
	}
	old := root.task.budget.rootTokens
	before := old.used.Load()
	cancel()
	if err := root.Run(t.Context(), "new task"); err != nil {
		t.Fatal(err)
	}
	current := root.task.budget.rootTokens
	if current == old {
		t.Fatal("new task kept old scope")
	}
	currentBefore := current.used.Load()
	child := New(&scriptedSummarizer{replies: []scriptedReply{{text: "## Standing facts\n- keep public APIs", usage: billed(1000, 100)}}}, tool.NewRegistry(), foldableSessionOverForce(30), Options{ContextWindow: 200000, RecentKeep: 2}, event.Discard)
	if _, err := child.CompactNow(capture.projected, CompactRequest{IgnoreEconomics: true}); err != nil {
		t.Fatal(err)
	}
	if old.used.Load() != before+1100 || current.used.Load() != currentBefore {
		t.Fatalf("late maintenance charged wrong scope: old=%d/%d new=%d/%d", old.used.Load(), before, current.used.Load(), currentBefore)
	}
}
