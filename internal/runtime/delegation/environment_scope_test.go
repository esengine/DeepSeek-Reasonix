package delegation

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/tools/builtin"
	"reasonix/internal/tools/jobs"
)

func TestEnvironmentBindsToolsAgainstTargetSandbox(t *testing.T) {
	root := t.TempDir()
	registry := tool.NewRegistry()
	registry.Add((builtin.Workspace{Dir: root, Bash: sandbox.Spec{Mode: "enforce", WriteRoots: []string{root}}}).Tools("bash")[0])
	p := &blockingProvider{started: make(chan struct{})}
	parent := NewTaskToolWithOptions(TaskToolOptions{Provider: p, ParentRegistry: registry, MaxSteps: 20, SysPrompt: "sys"}).WithTranscripts(NewSubagentStore(t.TempDir()), root, "model", "high")
	prepared := false
	parent.WithEnvironmentFactory("shared", func(_ context.Context, spec ProfileExecSpec, _ *EnvironmentSource) (*ExecutionEnvironment, error) {
		prepared = true
		child := NewTaskToolWithOptions(TaskToolOptions{Provider: p, ParentRegistry: registry, MaxSteps: 20, SysPrompt: "sys"}).WithTranscripts(parent.transcripts, root, "model", "high").WithBashSandboxEnforced(func() bool { return true })
		return &ExecutionEnvironment{ID: "iso_target", Task: child, Spec: spec}, nil
	})
	_, err := parent.Execute(testTaskContext(), json.RawMessage(`{"prompt":"inspect through bash","isolation":"worktree","tools":["bash"],"write_paths":["out.txt"]}`))
	if err != nil || !prepared {
		t.Fatalf("source sandbox rejected a target capability: prepared=%v err=%v", prepared, err)
	}
}

type sourceEscapeApproval struct{}

func (sourceEscapeApproval) ApproveSandboxEscape(context.Context, sandbox.EscapeRequest) (bool, string, error) {
	return true, "source session", nil
}

func TestBackgroundEnvironmentDoesNotInheritSourceHostCapabilities(t *testing.T) {
	root := t.TempDir()
	p := &blockingProvider{started: make(chan struct{})}
	parent := NewTaskToolWithOptions(TaskToolOptions{Provider: p, ParentRegistry: tool.NewRegistry(), MaxSteps: 20, SysPrompt: "sys"}).WithTranscripts(NewSubagentStore(t.TempDir()), root, "model", "high")
	parent.maxSubagentDepth = 4
	parent.WithEnvironmentFactory("shared", func(ctx context.Context, spec ProfileExecSpec, _ *EnvironmentSource) (*ExecutionEnvironment, error) {
		if _, ok := tool.BackgroundStarterFrom(ctx); ok {
			t.Error("candidate inherited source background starter")
		}
		if _, ok := sandbox.EscapeApproverFrom(ctx); ok {
			t.Error("candidate inherited source sandbox escape")
		}
		if agent.ParentSession(ctx) != "parent-session" || agent.SubagentDepth(ctx) != 2 {
			t.Error("background preparation lost execution identity/depth")
		}
		child := NewTaskToolWithOptions(TaskToolOptions{Provider: p, ParentRegistry: tool.NewRegistry(), MaxSteps: 20, SysPrompt: "sys"}).WithTranscripts(parent.transcripts, root, "model", "high")
		child.maxSubagentDepth = 4
		return &ExecutionEnvironment{ID: "iso_target", Task: child, Spec: spec}, nil
	})
	manager := jobs.NewManager(event.Discard)
	defer manager.Close()
	ctx := agent.WithSubagentDepth(testTaskContext(), 2)
	ctx = tool.WithBackgroundStarter(ctx, func(string, string, func(context.Context, io.Writer) (string, error)) string { return "source-job" })
	ctx = sandbox.WithEscapeApprover(ctx, sourceEscapeApproval{})
	ctx = jobs.WithSession(jobs.WithManager(ctx, manager), "parent-session")
	out, err := parent.Execute(ctx, json.RawMessage(`{"prompt":"work","isolation":"worktree","run_in_background":true}`))
	if err != nil {
		t.Fatal(err)
	}
	results, _ := manager.WaitForSession(ctx, "parent-session", []string{extractJobID(out)}, jobs.WaitOptions{Timeout: 5 * time.Second})
	if len(results) != 1 || results[0].Status != jobs.Done {
		t.Fatalf("job: %+v", results)
	}
}
