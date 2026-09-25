package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// wholeWorkspaceDelegate stands in for run_skill/task at depth 0: its Execute
// takes a whole-workspace writer slot and drives a depth-1 child that writes.
type wholeWorkspaceDelegate struct {
	sched      *SubagentScheduler
	root       string
	acquireErr error
	strayErr   error
	child      toolOutcome
}

func (*wholeWorkspaceDelegate) Name() string            { return "delegate" }
func (*wholeWorkspaceDelegate) Description() string     { return "delegate" }
func (*wholeWorkspaceDelegate) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (*wholeWorkspaceDelegate) ReadOnly() bool          { return false }

func (d *wholeWorkspaceDelegate) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	whole, err := WholeWorkspaceWriteClaim(d.root)
	if err != nil {
		return "", err
	}
	release, id, err := d.sched.AcquireWithID(ctx, AcquireRequest{Writer: true, WritePaths: whole})
	d.acquireErr = err
	if err != nil {
		return "", err
	}
	defer release()
	_, d.strayErr = d.sched.Acquire(context.Background(), AcquireRequest{Writer: true, WritePaths: whole, Nested: true})

	reg := tool.NewRegistry()
	reg.Add(&recordingWriter{name: "write_file"})
	child := New(nil, reg, NewSession(""), Options{
		WriteScheduler:     d.sched,
		WriteWorkspaceRoot: d.root,
		SubagentDepth:      1,
	}, event.Discard)
	d.child = child.executeOne(WithSubagentClaimID(ctx, id), &child.turn, provider.ToolCall{
		ID:        "child-write",
		Name:      "write_file",
		Arguments: `{"path":"` + filepath.ToSlash(filepath.Join(d.root, "out.md")) + `","content":"x"}`,
	})
	return "delegated", nil
}

func TestDepthZeroDelegationIsNotBlockedByItsOwnHookParentClaim(t *testing.T) {
	root := t.TempDir()
	sched := NewSubagentScheduler(4, 2)
	delegate := &wholeWorkspaceDelegate{sched: sched, root: root}
	reg := tool.NewRegistry()
	reg.Add(delegate)
	a := New(nil, reg, NewSession(""), Options{
		Hooks:              &parentClaimProbeHooks{scheduler: sched},
		WriteScheduler:     sched,
		WriteWorkspaceRoot: root,
	}, event.Discard)

	done := make(chan toolOutcome, 1)
	go func() {
		done <- a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "d-1", Name: "delegate", Arguments: `{}`})
	}()
	var out toolOutcome
	select {
	case out = <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("depth-0 delegation still waiting on the parent claim its own tool call holds; queued claims=%d", len(sched.ActiveWriterClaims()))
	}
	if delegate.acquireErr != nil || out.errMsg != "" {
		t.Fatalf("delegation failed: acquire=%v outcome=%+v", delegate.acquireErr, out)
	}
	if delegate.child.blocked || delegate.child.errMsg != "" {
		t.Fatalf("child write under the delegated slot was blocked: %+v", delegate.child)
	}
	if delegate.strayErr == nil {
		t.Fatal("an unrelated whole-workspace writer started while the delegation held the workspace")
	}
	if n := len(sched.ActiveWriterClaims()); n != 0 {
		t.Fatalf("claims after delegation = %d, want 0", n)
	}
}

func TestDelegationInsideParentClaimIsNotQueuedBehindWritersThatClaimBlocks(t *testing.T) {
	root := t.TempDir()
	sched := NewSubagentScheduler(4, 2)
	whole, err := WholeWorkspaceWriteClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	releaseParent, parentID, err := sched.ReserveParentWriteWithID(whole)
	if err != nil {
		t.Fatal(err)
	}
	queued := make(chan error, 1)
	go func() {
		release, err := sched.Acquire(context.Background(), AcquireRequest{Writer: true, WritePaths: whole})
		if err == nil {
			release()
		}
		queued <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		sched.mu.Lock()
		n := len(sched.waiters)
		sched.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background writer never queued")
		}
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(WithParentWriteClaimID(context.Background(), parentID), 5*time.Second)
	defer cancel()
	releaseDelegate, err := sched.Acquire(ctx, AcquireRequest{Writer: true, WritePaths: whole})
	if err != nil {
		t.Fatalf("delegation queued behind a writer its own parent claim blocks: %v", err)
	}
	releaseDelegate()
	releaseParent()
	if err := <-queued; err != nil {
		t.Fatalf("queued writer after parent release: %v", err)
	}
}
