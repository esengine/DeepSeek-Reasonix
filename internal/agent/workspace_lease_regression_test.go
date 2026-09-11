package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/tool/builtin"
	"reasonix/internal/workspacelease"
)

type workspaceWritingHooks struct {
	path  string
	calls atomic.Int32
}

type blockingWorkspaceLeaseMetaTool struct {
	started chan struct{}
	release chan struct{}
}

func (*blockingWorkspaceLeaseMetaTool) Name() string        { return "fleet" }
func (*blockingWorkspaceLeaseMetaTool) Description() string { return "fleet test double" }
func (*blockingWorkspaceLeaseMetaTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (*blockingWorkspaceLeaseMetaTool) ReadOnly() bool { return false }
func (t *blockingWorkspaceLeaseMetaTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	close(t.started)
	select {
	case <-t.release:
		return "ok", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (h *workspaceWritingHooks) PreToolUse(context.Context, string, json.RawMessage) (bool, string) {
	h.calls.Add(1)
	_ = os.WriteFile(h.path, []byte("hook"), 0o600)
	return false, ""
}

func (*workspaceWritingHooks) PostToolUse(context.Context, string, json.RawMessage, string) {}
func (*workspaceWritingHooks) PostToolUseFailure(context.Context, string, json.RawMessage, string, error) {
}
func (*workspaceWritingHooks) PostLLMCall(_ context.Context, reasoning string, _ int) string {
	return reasoning
}
func (*workspaceWritingHooks) HasPostLLMCall() bool                      { return false }
func (*workspaceWritingHooks) SubagentStop(context.Context, string)      {}
func (*workspaceWritingHooks) PreCompact(context.Context, string) string { return "" }

func TestFleetMetaToolDoesNotTakeOuterWorkspaceLease(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	fleetOwner, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	probeOwner, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	fleetOwner.BeginRun()
	probeOwner.BeginRun()
	defer fleetOwner.EndRun()
	defer probeOwner.EndRun()

	fleet := &blockingWorkspaceLeaseMetaTool{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	t.Cleanup(func() {
		select {
		case <-fleet.release:
		default:
			close(fleet.release)
		}
	})
	a := deliveryLeaseTestAgent(t, fleetOwner, fleet)
	a.writeWorkspaceRoot = root
	done := make(chan toolOutcome, 1)
	go func() {
		done <- a.executeOne(context.Background(), &a.turn, providerToolCall("fleet", fleet.Name()))
	}()

	select {
	case <-fleet.started:
	case <-time.After(time.Second):
		t.Fatal("fleet did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	releaseProbe, err := probeOwner.HoldWrite(ctx)
	cancel()
	if err != nil {
		t.Fatalf("fleet meta call held an outer workspace lease: %v", err)
	}
	releaseProbe()
	close(fleet.release)

	select {
	case out := <-done:
		if out.blocked || out.errMsg != "" {
			t.Fatalf("fleet outcome = %+v", out)
		}
	case <-time.After(time.Second):
		t.Fatal("fleet did not return after release")
	}
}

func TestKillShellDoesNotWaitForBackgroundWriterLease(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	owner, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner.BeginRun()
	defer owner.EndRun()

	manager := jobs.NewManager(event.Discard)
	defer manager.Close()
	leaseHeld := make(chan struct{})
	job := manager.StartForSession("parent-session", "task", "writer", func(ctx context.Context, _ io.Writer) (string, error) {
		release, err := owner.HoldWriteForPath(ctx, filepath.Join(root, "held.go"))
		if err != nil {
			return "", err
		}
		defer release()
		close(leaseHeld)
		<-ctx.Done()
		return "", ctx.Err()
	})
	select {
	case <-leaseHeld:
	case <-time.After(time.Second):
		t.Fatal("background writer did not acquire its path lease")
	}

	tools := (builtin.Workspace{Dir: root}).Tools("kill_shell")
	if len(tools) != 1 {
		t.Fatalf("kill_shell tools = %d, want 1", len(tools))
	}
	a := deliveryLeaseTestAgent(t, owner, tools[0])
	a.svc.jobs = manager
	a.writeWorkspaceRoot = root

	ctx := jobs.WithManager(context.Background(), manager)
	ctx = jobs.WithSession(ctx, "parent-session")
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	args, err := json.Marshal(map[string]string{"job_id": job.ID})
	if err != nil {
		t.Fatal(err)
	}
	out := a.executeOne(ctx, &a.turn, provider.ToolCall{ID: "kill", Name: "kill_shell", Arguments: string(args)})
	if out.blocked || out.errMsg != "" {
		t.Fatalf("kill_shell waited for the background writer lease: %+v", out)
	}
	result := manager.WaitForSession(context.Background(), "parent-session", []string{job.ID}, 1)
	if len(result) != 1 || result[0].Status != jobs.Killed {
		t.Fatalf("background job after kill_shell = %+v, want killed", result)
	}
}

func TestWritableHooksUseWorkspaceLease(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	holder, _ := workspacelease.New(root, locks, nil)
	writerOwner, _ := workspacelease.New(root, locks, nil)
	protected := filepath.Join(root, "protected.go")
	releaseProtected, err := holder.HoldWriteForPath(context.Background(), protected)
	if err != nil {
		t.Fatal(err)
	}

	writer := &workspaceLeaseTestTool{name: "lease_reader", readOnly: true}
	hooks := &workspaceWritingHooks{path: protected}
	a := deliveryLeaseTestAgent(t, writerOwner, writer)
	a.writeWorkspaceRoot = root
	a.svc.hooks = hooks
	call := providerToolCall("write", writer.Name())
	call.Arguments = `{"path":"probe.go","content":"probe"}`
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	out := a.executeOne(ctx, &a.turn, call)
	cancel()
	if !out.blocked || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("hook-capable writer was not blocked by active workspace write: %+v", out)
	}
	if got := hooks.calls.Load(); got != 0 {
		t.Fatalf("hook ran %d times before the workspace became exclusive", got)
	}

	releaseProtected()
	out = a.executeOne(context.Background(), &a.turn, call)
	if out.blocked || out.errMsg != "" {
		t.Fatalf("writer after release: %+v", out)
	}
	if got := hooks.calls.Load(); got != 1 {
		t.Fatalf("hook calls = %d, want 1", got)
	}
}

func TestWritableHooksReserveWholeParentWorkspace(t *testing.T) {
	root := t.TempDir()
	scheduler := NewSubagentScheduler(4, 2)
	hookClaim, err := NormalizeWritePaths(root, []string{"hook-side.go"})
	if err != nil {
		t.Fatal(err)
	}
	hooks := &parentClaimProbeHooks{scheduler: scheduler, claim: hookClaim}
	writer := &recordingWriter{name: "lease_reader", readOnly: true}
	a := deliveryLeaseTestAgent(t, nil, writer)
	a.svc.hooks = hooks
	a.svc.writeScheduler = scheduler
	a.writeWorkspaceRoot = root
	call := providerToolCall("write", writer.Name())
	call.Arguments = `{"path":"probe.go","content":"probe"}`
	out := a.executeOne(context.Background(), &a.turn, call)
	if out.blocked || out.errMsg != "" {
		t.Fatalf("executeOne failed: %+v", out)
	}
	if hooks.acquireErr == nil {
		t.Fatal("hook-side path bypassed the parent workspace reservation")
	}
}

func TestWritableHooksSerializeReadOnlyToolBatch(t *testing.T) {
	root := t.TempDir()
	first := &workspaceLeaseTestTool{name: "lease_reader_a", readOnly: true}
	second := &workspaceLeaseTestTool{name: "lease_reader_b", readOnly: true}
	hooks := &workspaceWritingHooks{path: filepath.Join(root, "hook-side.go")}
	a := deliveryLeaseTestAgent(t, nil, first, second)
	a.svc.writeScheduler = NewSubagentScheduler(4, 2)
	a.writeWorkspaceRoot = root
	calls := []provider.ToolCall{
		providerToolCall("read-a", first.Name()),
		providerToolCall("read-b", second.Name()),
	}

	if batches := a.toolCallBatches(calls); len(batches) != 1 || !batches[0].parallel {
		t.Fatalf("ordinary read batches = %+v, want unchanged parallel fan-out", batches)
	}
	a.svc.hooks = hooks
	batches := a.toolCallBatches(calls)
	if len(batches) != 1 || batches[0].parallel {
		t.Fatalf("hook-capable read batches = %+v, want one serial batch", batches)
	}
	result := a.executeBatch(context.Background(), &a.turn, calls)
	for i, outcome := range result.outcomes {
		if outcome.blocked || outcome.errMsg != "" {
			t.Fatalf("read-only call %d was dropped by hook coordination: %+v", i, outcome)
		}
	}
	if first.calls.Load() != 1 || second.calls.Load() != 1 || hooks.calls.Load() != 2 {
		t.Fatalf("executions first=%d second=%d hooks=%d, want 1/1/2",
			first.calls.Load(), second.calls.Load(), hooks.calls.Load())
	}
}
