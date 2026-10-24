package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/mutation"
	"reasonix/internal/tools/jobs"
)

func TestBackgroundBashIsAdmittedBeforeLaunchAndReleasesOnceStarted(t *testing.T) {
	root := t.TempDir()
	notices := make(chan mutation.Notice, 20)
	c, err := mutation.New(root, t.TempDir(), func(n mutation.Notice) { notices <- n })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	parent, err := c.Acquire(ctx, mutation.Intent{Effect: mutation.Workspace, CallID: "submit"})
	if err != nil {
		t.Fatal(err)
	}
	defer parent.End(mutation.Unknown)
	manager := jobs.NewManager(event.Discard)
	defer manager.Close()
	callCtx := parent.Context(jobs.WithSession(jobs.WithManager(ctx, manager), "session"))
	sh := sandbox.ResolveShell("", "", nil)
	command := "echo ready > ready; sleep 120"
	if sh.Kind == sandbox.ShellPowerShell {
		command = "Set-Content ready ready; Start-Sleep -Seconds 120"
	}
	b := bash{workDir: root, sb: sandbox.Spec{Mode: "off"}, shell: sh}
	out, err := b.ExecuteDetailed(callCtx, argsJSON(t, map[string]any{"command": command, "run_in_background": true}))
	if err != nil {
		t.Fatal(err)
	}
	jobID := backgroundJobIDFromStartOutput(t, out.Output)
	waitMutationNotice(t, ctx, notices, "submit", "waiting")
	if _, err := os.Stat(filepath.Join(root, "ready")); !os.IsNotExist(err) {
		t.Fatalf("process started before its own lease: %v", err)
	}
	parent.End(mutation.NotExecuted)
	waitMutationNotice(t, ctx, notices, "submit", "acquired")
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("process never started")
		}
	}
	waitMutationNotice(t, ctx, notices, "submit", "released")
	contender, err := c.TryAcquire(ctx, mutation.Intent{Effect: mutation.Workspace, CallID: "contender"})
	if err != nil {
		t.Fatalf("a running background process kept the workspace transaction: %v", err)
	}
	contender.End(mutation.NotExecuted)
	if _, status, ok := manager.OutputForSession("session", jobID); !ok || status != jobs.Running {
		t.Fatalf("background process ended before the contender was admitted: %s", status)
	}
	manager.KillForSession("session", jobID)
}

func waitMutationNotice(t *testing.T, ctx context.Context, notices <-chan mutation.Notice, call, state string) {
	t.Helper()
	for {
		select {
		case n := <-notices:
			if n.CallID == call && n.State == state {
				return
			}
		case <-ctx.Done():
			t.Fatalf("missing %s %s event", call, state)
		}
	}
}

func TestCoordinatedBashRefusesUnownedProcessLifetime(t *testing.T) {
	root := t.TempDir()
	c, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := c.Acquire(t.Context(), mutation.Intent{Effect: mutation.Workspace})
	if err != nil {
		t.Fatal(err)
	}
	defer l.End(mutation.NotExecuted)
	b := bash{workDir: root, sb: sandbox.Spec{Mode: "off"}}
	for _, args := range []map[string]any{
		{"command": "echo ready", "preserve_background_processes": true},
		{"command": "nohup sleep 120 &"},
	} {
		out, err := b.ExecuteDetailed(l.Context(t.Context()), argsJSON(t, args))
		var refusal tool.Refusal
		if !errors.As(err, &refusal) || refusal.Code != "workspace.detached_mutation" || out.Execution.State != tool.ShellStateNotRun {
			t.Fatalf("unowned lifetime accepted: %+v %v", out, err)
		}
	}
}

func TestQueuedBackgroundBashCancellationNeverStartsProcess(t *testing.T) {
	root := t.TempDir()
	notices := make(chan mutation.Notice, 20)
	c, err := mutation.New(root, t.TempDir(), func(n mutation.Notice) { notices <- n })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	l, err := c.Acquire(ctx, mutation.Intent{Effect: mutation.Workspace, CallID: "submit"})
	if err != nil {
		t.Fatal(err)
	}
	defer l.End(mutation.NotExecuted)
	manager := jobs.NewManager(event.Discard)
	defer manager.Close()
	sh := sandbox.ResolveShell("", "", nil)
	command := "echo ready > ready"
	if sh.Kind == sandbox.ShellPowerShell {
		command = "Set-Content ready ready"
	}
	b := bash{workDir: root, shell: sh, sb: sandbox.Spec{Mode: "off"}}
	callCtx := l.Context(jobs.WithSession(jobs.WithManager(ctx, manager), "session"))
	out, err := b.ExecuteDetailed(callCtx, argsJSON(t, map[string]any{"command": command, "run_in_background": true}))
	if err != nil {
		t.Fatal(err)
	}
	id := backgroundJobIDFromStartOutput(t, out.Output)
	waitMutationNotice(t, ctx, notices, "submit", "waiting")
	if !manager.KillForSession("session", id) {
		t.Fatal("queued job cannot be cancelled")
	}
	waitMutationNotice(t, ctx, notices, "submit", "abandoned")
	l.End(mutation.NotExecuted)
	next, err := c.Acquire(ctx, mutation.Intent{Effect: mutation.Workspace})
	if err != nil {
		t.Fatal(err)
	}
	next.End(mutation.NotExecuted)
	if _, err := os.Stat(filepath.Join(root, "ready")); !os.IsNotExist(err) {
		t.Fatalf("cancelled queued process wrote: %v", err)
	}
}
