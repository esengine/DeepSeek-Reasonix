package delegation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/tools/builtin"
	"reasonix/internal/tools/jobs"
)

func TestEnvironmentEvidenceDoesNotReachSourceLedger(t *testing.T) {
	t.Run("foreground", func(t *testing.T) { testEnvironmentEvidence(t, false, false) })
	t.Run("background", func(t *testing.T) { testEnvironmentEvidence(t, true, false) })
	t.Run("foreground-panic", func(t *testing.T) { testEnvironmentEvidence(t, false, true) })
	t.Run("background-panic", func(t *testing.T) { testEnvironmentEvidence(t, true, true) })
}

func TestNestedEnvironmentUsesCurrentCandidate(t *testing.T) {
	task := &TaskTool{environmentID: "iso_current", environment: &environmentPolicy{mode: "auto"}}
	for _, mode := range []string{"", "auto", "shared"} {
		got, err := task.executionEnvironment(ProfileExecSpec{Sched: SchedulerPolicy{Environment: mode, RunInBackground: true}})
		if err != nil || got != "shared" {
			t.Fatalf("nested %q: %q %v", mode, got, err)
		}
	}
	if _, err := task.executionEnvironment(ProfileExecSpec{Sched: SchedulerPolicy{Environment: "worktree"}}); err == nil {
		t.Fatal("nested worktree accepted without an integration route")
	}
}

func TestNestedEnvironmentUsesRegisteredFactoryWithoutChangingSharedOrReadonly(t *testing.T) {
	task := &TaskTool{environmentID: "iso_current", environment: &environmentPolicy{mode: "auto", prepare: func(context.Context, ProfileExecSpec, *EnvironmentSource) (*ExecutionEnvironment, error) {
		return nil, nil
	}}}
	for _, mode := range []string{"auto", "shared", "worktree", "isolated"} {
		want := mode
		if mode == "isolated" {
			want = "worktree"
		}
		got, err := task.executionEnvironment(ProfileExecSpec{Sched: SchedulerPolicy{Environment: mode}})
		if err != nil || got != want {
			t.Fatalf("nested factory %s: %s %v", mode, got, err)
		}
		readonly, err := task.executionEnvironment(ProfileExecSpec{Grant: CapabilityGrant{ReadOnly: true}, Sched: SchedulerPolicy{Environment: mode}})
		if err != nil || readonly != "shared" {
			t.Fatalf("readonly task prepared a candidate: %s %v", readonly, err)
		}
	}
}

func testEnvironmentEvidence(t *testing.T, background, panicAfterWrite bool) {
	t.Helper()
	root := t.TempDir()
	reg := tool.NewRegistry()
	for _, writer := range builtin.ConfineWriters([]string{root}, builtin.SessionDataGuard{}, builtin.ManagedConfigPaths{}) {
		reg.Add(writer)
	}
	args, _ := json.Marshal(map[string]string{"path": filepath.Join(root, "out.txt"), "content": "candidate"})
	var p provider.Provider = &mockProvider{name: "environment", streams: [][]provider.Chunk{
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "w", Name: "write_file", Arguments: string(args)}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	if panicAfterWrite {
		p = &panickingEnvironmentProvider{Provider: p}
	}
	child := NewTaskToolWithOptions(TaskToolOptions{Provider: p, ParentRegistry: reg, MaxSteps: 20, SysPrompt: "sys", CheckTargetAccess: allowTestFileAccess}).
		WithTranscripts(NewSubagentStore(t.TempDir()), root, "model", "high").WithOptimisticClaims(true)
	parent := NewTaskToolWithOptions(TaskToolOptions{Provider: p, ParentRegistry: reg, MaxSteps: 20, SysPrompt: "sys"}).
		WithTranscripts(NewSubagentStore(t.TempDir()), root, "model", "high").WithOptimisticClaims(true)
	finished := 0
	candidateEvidence := evidence.NewLedger()
	parent.WithEnvironmentFactory("shared", func(_ context.Context, spec ProfileExecSpec, _ *EnvironmentSource) (*ExecutionEnvironment, error) {
		if spec.Worker.Model != "model" || spec.Worker.Effort != "high" {
			t.Errorf("environment lost inherited runtime selection: %q/%q", spec.Worker.Model, spec.Worker.Effort)
		}
		return &ExecutionEnvironment{ID: "iso_test", Task: child, Spec: spec, Evidence: candidateEvidence, Finish: func(_ context.Context, answer string, err error) (string, error) {
			finished++
			return answer, err
		}}, nil
	})
	ledger := evidence.NewLedger()
	ctx := evidence.WithLedger(testTaskContext(), ledger)
	jm := jobs.NewManager(event.Discard)
	defer jm.Close()
	ctx = jobs.WithSession(jobs.WithManager(ctx, jm), "parent-session")
	call, _ := json.Marshal(map[string]any{"prompt": "create out.txt", "isolation": "worktree", "run_in_background": background})
	out, err := parent.Execute(ctx, call)
	if (err != nil) != (panicAfterWrite && !background) {
		t.Fatal(err)
	}
	if background {
		status := jobs.Done
		if panicAfterWrite {
			status = jobs.Failed
		}
		results, outcome := jm.WaitForSession(ctx, "parent-session", []string{extractJobID(out)}, jobs.WaitOptions{Timeout: 5 * time.Second})
		if len(results) != 1 || results[0].Status != status {
			t.Fatalf("background result: %+v %v", results, outcome)
		}
		if got := jm.LeaseEvidenceForSession("parent-session", extractJobID(out)); len(got.Receipts) != 0 {
			t.Fatalf("background job published candidate evidence to source: %+v", got)
		}
	}
	if content, err := os.ReadFile(filepath.Join(root, "out.txt")); err != nil || string(content) != "candidate" {
		t.Fatalf("candidate write: %q %v", content, err)
	}
	if finished != 1 {
		t.Fatalf("environment finalized %d times", finished)
	}
	if got := ledger.Summary(); len(got.Receipts) != 0 {
		t.Fatalf("source ledger contains candidate evidence: %+v", got)
	}
	if got := candidateEvidence.Summary(); !got.HasMutation() {
		t.Fatalf("candidate evidence was lost: %+v", got)
	}
}

type panickingEnvironmentProvider struct {
	provider.Provider
	calls int
}

func (p *panickingEnvironmentProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	if p.calls > 1 {
		panic("provider failed after write")
	}
	return p.Provider.Stream(ctx, req)
}
