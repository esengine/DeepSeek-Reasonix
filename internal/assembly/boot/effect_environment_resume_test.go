package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/session/control"
	"reasonix/internal/tools/jobs"
)

type resumedEnvironmentProvider struct{ calls atomic.Int32 }

func (p *resumedEnvironmentProvider) Name() string { return "resumed-environment" }

func (p *resumedEnvironmentProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	call := p.calls.Add(1) - 1
	switch call {
	case 0, 2:
		path := "out.txt"
		if call == 2 {
			path = "second.txt"
		}
		raw, _ := json.Marshal(map[string]string{"path": path, "content": "candidate"})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "write", Name: "write_file", Arguments: string(raw)}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectEnvironmentContinuationReusesCandidate(t *testing.T) {
	home, root := statedBootHome(t), testenv.TempDir(t)
	p := &resumedEnvironmentProvider{}
	provider.Register("boot-resume-environment", func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
collaboration = "shared"
max_subagent_concurrency = 1
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-resume-environment"
model = "x"
`)
	mirrorToUserConfigAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-q", "-m", "init")
	b := newBuilder()
	built, err := b.build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(built.Controller.Close)
	if err := os.MkdirAll(b.session.dir, 0700); err != nil {
		t.Fatal(err)
	}
	built.Controller.SetToolApprovalMode(control.ToolApprovalYolo)
	ctx := agent.WithParentSession(t.Context(), "owning-session")
	jm := jobs.NewManager(event.Discard)
	t.Cleanup(jm.Close)
	ctx = jobs.WithSession(jobs.WithManager(ctx, jm), "owning-session")
	args := map[string]any{"prompt": "write first result", "isolation": "worktree", "tools": []string{"write_file"}, "write_paths": []string{"out.txt", "second.txt"}}
	raw, _ := json.Marshal(args)
	out, err := b.tools.taskTool.Execute(ctx, raw)
	if err != nil {
		t.Fatalf("first run: %s %v", out, err)
	}
	ref := regexp.MustCompile(`sa_[a-zA-Z0-9_]+`).FindString(out)
	if ref == "" {
		t.Fatalf("missing transcript: %s", out)
	}
	before, err := b.tools.subagents.LoadMeta(ref)
	if err != nil {
		t.Fatal(err)
	}
	built.Controller.Close()
	b = newBuilder()
	built, err = b.build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(built.Controller.Close)
	built.Controller.SetToolApprovalMode(control.ToolApprovalYolo)
	args["continue_from"], args["prompt"] = ref, "write second result"
	args["run_in_background"] = true
	delete(args, "isolation")
	raw, _ = json.Marshal(args)
	runQueuedEnvironment(t, ctx, b, raw, p)
	after, err := b.tools.subagents.LoadMeta(ref)
	if err != nil {
		t.Fatal(err)
	}
	if before.EnvironmentID == "" || before.EnvironmentID != after.EnvironmentID || before.WorkspaceRoot != after.WorkspaceRoot {
		t.Fatalf("continuation changed environment: before=%+v after=%+v", before, after)
	}
	for _, path := range []string{"out.txt", "second.txt"} {
		if b, err := os.ReadFile(filepath.Join(after.WorkspaceRoot, path)); err != nil || string(b) != "candidate" {
			t.Fatalf("candidate %s: %q %v", path, b, err)
		}
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("source %s changed: %v", path, err)
		}
	}
	runs, err := b.tools.subagents.Runs("owning-session", b.root)
	if err != nil || len(runs) != 1 {
		t.Fatalf("source cannot list isolated transcript: %+v %v", runs, err)
	}
}

func runQueuedEnvironment(t *testing.T, ctx context.Context, b *builder, raw json.RawMessage, p *resumedEnvironmentProvider) {
	t.Helper()
	marks, err := filepath.Glob(filepath.Join(b.roots.DeliveryWorktreeDir(), "isolated", "*", "entry-v2.json"))
	if err != nil || len(marks) != 1 {
		t.Fatalf("candidate marker: %v %v", marks, err)
	}
	before, err := os.ReadFile(marks[0])
	if err != nil {
		t.Fatal(err)
	}
	release, err := b.tools.taskTool.Scheduler().Acquire(ctx, writeclaim.AcquireRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() { out, err := b.tools.taskTool.Execute(ctx, raw); done <- result{out, err} }()
	var started result
	select {
	case started = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("isolated background task waited for capacity before returning job id")
	}
	if started.err != nil {
		t.Fatalf("background start: %s %v", started.out, started.err)
	}
	if p.calls.Load() != 2 {
		t.Fatal("isolated task bypassed root scheduler")
	}
	after, err := os.ReadFile(marks[0])
	if err != nil || string(before) != string(after) {
		t.Fatalf("queued task changed candidate lifecycle before admission: %v", err)
	}
	jobID := regexp.MustCompile(`task-[0-9]+`).FindString(started.out)
	if jobID == "" {
		t.Fatalf("missing job id: %s", started.out)
	}
	release()
	jm, _ := jobs.FromContext(ctx)
	results, _ := jm.WaitForSession(ctx, "owning-session", []string{jobID}, jobs.WaitOptions{Timeout: 20 * time.Second})
	if len(results) != 1 || results[0].Status != jobs.Done {
		t.Fatalf("continued job: %+v", results)
	}
	if evidence := jm.LeaseEvidenceForSession("owning-session", jobID); len(evidence.Receipts) != 0 {
		t.Fatal("isolated job contaminated source evidence")
	}
}
