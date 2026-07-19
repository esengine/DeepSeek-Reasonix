package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
)

func TestBindWritePathsRebindsBashWriteRoots(t *testing.T) {
	root := t.TempDir()
	claim, err := NormalizeWritePaths(root, []string{"docs"})
	if err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	reg.Add(builtin.ConfineBash(sandbox.Spec{
		Mode:       "enforce",
		WriteRoots: []string{root},
	}, builtin.SessionDataGuard{}))
	reg.Add(foregroundOnlyBash{inner: mustGet(t, reg, "bash")})

	bound, removed := BindWritePaths(reg, claim, root, true)
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want none", removed)
	}
	if _, ok := bound.Get("bash"); !ok {
		t.Fatal("bash should be kept when sandbox can rebind")
	}

	_, removed = BindWritePaths(reg, claim, root, false)
	if len(removed) != 1 || removed[0] != "bash" {
		t.Fatalf("removed = %v, want [bash]", removed)
	}
}

func TestParentWriteReservationBlocksOverlappingSubagentAcquire(t *testing.T) {
	root := t.TempDir()
	sched := NewSubagentScheduler(4, 2)
	claim, err := parentWriteReservation(root, "write_file", mustJSON(t, map[string]string{
		"path":    filepath.Join(root, "a.md"),
		"content": "x",
	}))
	if err != nil {
		t.Fatal(err)
	}
	release, err := sched.ReserveParentWrite(claim)
	if err != nil {
		t.Fatal(err)
	}

	// Nested acquire must fail-fast while parent holds the path.
	subClaim, err := NormalizeWritePaths(root, []string{"a.md"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sched.Acquire(context.Background(), AcquireRequest{
		Writer: true, WritePaths: subClaim, Nested: true,
	})
	if err == nil {
		t.Fatal("subagent should not acquire path held by parent reservation")
	}
	release()

	// After release, acquire succeeds.
	rel2, err := sched.Acquire(context.Background(), AcquireRequest{
		Writer: true, WritePaths: subClaim,
	})
	if err != nil {
		t.Fatal(err)
	}
	rel2()
}

// TestParentWriteReservationClosesTOCTOU proves a parent reservation held for
// the whole Execute window prevents a concurrent subagent from claiming the
// same path after a check-but-before-write window would have opened.
func TestParentWriteReservationClosesTOCTOU(t *testing.T) {
	root := t.TempDir()
	sched := NewSubagentScheduler(4, 2)
	path := filepath.Join(root, "race.md")
	args := mustJSON(t, map[string]string{"path": path, "content": "parent"})

	parentStarted := make(chan struct{})
	releaseParent := make(chan struct{})
	parentDone := make(chan struct{})

	go func() {
		defer close(parentDone)
		claim, err := parentWriteReservation(root, "write_file", args)
		if err != nil {
			t.Errorf("parent reservation: %v", err)
			close(parentStarted)
			return
		}
		release, err := sched.ReserveParentWrite(claim)
		if err != nil {
			t.Errorf("ReserveParentWrite: %v", err)
			close(parentStarted)
			return
		}
		// Signal that the parent write has "started" (reservation held).
		close(parentStarted)
		// Hold the reservation while a concurrent subagent tries to claim.
		<-releaseParent
		release()
	}()

	<-parentStarted

	subClaim, err := NormalizeWritePaths(root, []string{"race.md"})
	if err != nil {
		t.Fatal(err)
	}
	// Non-nested would queue; Nested fail-fast proves conflict under reservation.
	_, err = sched.Acquire(context.Background(), AcquireRequest{
		Writer: true, WritePaths: subClaim, Nested: true,
	})
	if err == nil {
		t.Fatal("expected TOCTOU-safe rejection while parent write holds reservation")
	}
	if !strings.Contains(err.Error(), "parent write") && !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("unexpected error: %v", err)
	}
	close(releaseParent)
	<-parentDone
}

func TestAgentReservesParentWriteBeforePreToolUse(t *testing.T) {
	root := t.TempDir()
	sched := NewSubagentScheduler(4, 2)
	claim, err := NormalizeWritePaths(root, []string{"hook-race.md"})
	if err != nil {
		t.Fatal(err)
	}
	hooks := &parentClaimProbeHooks{scheduler: sched, claim: claim}
	writer := &recordingWriter{name: "write_file"}
	reg := tool.NewRegistry()
	reg.Add(writer)
	a := New(nil, reg, NewSession(""), Options{
		Hooks:              hooks,
		WriteScheduler:     sched,
		WriteWorkspaceRoot: root,
	}, event.Discard)

	out := a.executeOne(context.Background(), provider.ToolCall{
		ID:        "write-1",
		Name:      "write_file",
		Arguments: string(mustJSON(t, map[string]string{"path": "hook-race.md", "content": "parent"})),
	})
	if out.errMsg != "" {
		t.Fatalf("executeOne failed: %+v", out)
	}
	if hooks.acquireErr == nil {
		t.Fatal("PreToolUse hook observed no parent claim; reservation must precede hooks")
	}
	if writer.calls != 1 {
		t.Fatalf("writer calls = %d, want 1", writer.calls)
	}
	if n := len(sched.ActiveWriterClaims()); n != 0 {
		t.Fatalf("claims after Execute = %d, want 0", n)
	}
}

func TestParentWriteReservationBashClaimsWholeWorkspace(t *testing.T) {
	root := t.TempDir()
	claim, err := parentWriteReservation(root, "bash", json.RawMessage(`{"command":"echo hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !claim.WholeWorkspace {
		t.Fatalf("bash reservation must claim whole workspace, got %+v", claim)
	}
	mcp, err := parentWriteReservation(root, "mcp__srv__write", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !mcp.WholeWorkspace {
		t.Fatalf("MCP writer reservation must claim whole workspace")
	}
}

func TestAgentReserveParentWriteSkipsSubagentDepth(t *testing.T) {
	root := t.TempDir()
	sched := NewSubagentScheduler(4, 2)
	a := &Agent{
		writeScheduler:     sched,
		writeWorkspaceRoot: root,
		subagentDepth:      1,
	}
	inner := &recordingWriter{name: "write_file"}
	release, err := a.reserveParentWrite(inner, mustJSON(t, map[string]string{
		"path": filepath.Join(root, "a.md"), "content": "x",
	}), false)
	if err != nil {
		t.Fatal(err)
	}
	release()
	// No parent claim should remain — subagent depth skips reservation.
	if n := len(sched.ActiveWriterClaims()); n != 0 {
		t.Fatalf("claims = %d, want 0", n)
	}
}

func TestAgentReserveParentWriteHoldsClaim(t *testing.T) {
	root := t.TempDir()
	sched := NewSubagentScheduler(4, 2)
	a := &Agent{
		writeScheduler:     sched,
		writeWorkspaceRoot: root,
		subagentDepth:      0,
	}
	inner := &recordingWriter{name: "write_file"}
	release, err := a.reserveParentWrite(inner, mustJSON(t, map[string]string{
		"path": filepath.Join(root, "a.md"), "content": "x",
	}), false)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(sched.ActiveWriterClaims()); n != 1 {
		t.Fatalf("claims = %d, want 1", n)
	}
	release()
	if n := len(sched.ActiveWriterClaims()); n != 0 {
		t.Fatalf("claims after release = %d", n)
	}
}

func mustGet(t *testing.T, reg *tool.Registry, name string) tool.Tool {
	t.Helper()
	tl, ok := reg.Get(name)
	if !ok {
		t.Fatalf("missing %s", name)
	}
	return tl
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type recordingWriter struct {
	name     string
	readOnly bool
	calls    int
}

type parentClaimProbeHooks struct {
	scheduler  *SubagentScheduler
	claim      WritePathSet
	acquireErr error
}

func (h *parentClaimProbeHooks) PreToolUse(context.Context, string, json.RawMessage) (bool, string) {
	release, err := h.scheduler.Acquire(context.Background(), AcquireRequest{
		Writer: true, WritePaths: h.claim, Nested: true,
	})
	h.acquireErr = err
	if err == nil {
		release()
	}
	return false, ""
}
func (*parentClaimProbeHooks) PostToolUse(context.Context, string, json.RawMessage, string) {}
func (*parentClaimProbeHooks) PostToolUseFailure(context.Context, string, json.RawMessage, string, error) {
}
func (*parentClaimProbeHooks) PostLLMCall(_ context.Context, reasoning string, _ int) string {
	return reasoning
}
func (*parentClaimProbeHooks) HasPostLLMCall() bool                      { return false }
func (*parentClaimProbeHooks) SubagentStop(context.Context, string)      {}
func (*parentClaimProbeHooks) PreCompact(context.Context, string) string { return "" }

func (r *recordingWriter) Name() string        { return r.name }
func (r *recordingWriter) Description() string { return r.name }
func (r *recordingWriter) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}
func (r *recordingWriter) ReadOnly() bool { return r.readOnly }
func (r *recordingWriter) Execute(context.Context, json.RawMessage) (string, error) {
	r.calls++
	return "ok", nil
}
