package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

func TestParentWriteReservationBlocksOverlappingSubagentAcquire(t *testing.T) {
	root := testenv.TempDir(t)
	sched := writeclaim.NewSubagentScheduler(4, 2)
	claim, err := parentWriteReservation(root, "write_file", mustJSON(t, map[string]string{
		"path":    filepath.Join(root, "a.md"),
		"content": "x",
	}))
	if err != nil {
		t.Fatal(err)
	}
	release, err := sched.ReserveWrite(claim)
	if err != nil {
		t.Fatal(err)
	}

	// Nested acquire must fail-fast while parent holds the path.
	subClaim, err := writeclaim.NormalizeWritePaths(root, []string{"a.md"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sched.Acquire(context.Background(), writeclaim.AcquireRequest{
		Writer: true, WritePaths: subClaim, Nested: true,
	})
	if err == nil {
		t.Fatal("subagent should not acquire path held by parent reservation")
	}
	release()

	// After release, acquire succeeds.
	rel2, err := sched.Acquire(context.Background(), writeclaim.AcquireRequest{
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
	root := testenv.TempDir(t)
	sched := writeclaim.NewSubagentScheduler(4, 2)
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
		release, err := sched.ReserveWrite(claim)
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

	subClaim, err := writeclaim.NormalizeWritePaths(root, []string{"race.md"})
	if err != nil {
		t.Fatal(err)
	}
	// Non-nested would queue; Nested fail-fast proves conflict under reservation.
	_, err = sched.Acquire(context.Background(), writeclaim.AcquireRequest{
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
	root := testenv.TempDir(t)
	sched := writeclaim.NewSubagentScheduler(4, 2)
	claim, err := writeclaim.NormalizeWritePaths(root, []string{"hook-race.md"})
	if err != nil {
		t.Fatal(err)
	}
	hooks := &parentClaimProbeHooks{scheduler: sched, claim: claim}
	writer := &recordingWriter{name: "write_file", writesPaths: true}
	reg := tool.NewRegistry()
	reg.Add(writer)
	a := New(nil, reg, sessionstore.NewSession(""), Options{
		Hooks:              hooks,
		WriteScheduler:     sched,
		WriteWorkspaceRoot: root,

		CheckTargetAccess: allowTestFileAccess,
	}, event.Discard)

	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
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
	root := testenv.TempDir(t)
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
	root := testenv.TempDir(t)
	sched := writeclaim.NewSubagentScheduler(4, 2)
	a := &Agent{agentConfig: agentConfig{writeWorkspaceRoot: root, subagentDepth: 1}, svc: agentServices{writeScheduler: sched}}
	inner := &recordingWriter{name: "write_file", writesPaths: true}
	release, err := a.reserveParentWrite(inner, mustJSON(t, map[string]string{
		"path": filepath.Join(root, "a.md"), "content": "x",
	}), false, nil)
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
	root := testenv.TempDir(t)
	sched := writeclaim.NewSubagentScheduler(4, 2)
	a := &Agent{agentConfig: agentConfig{writeWorkspaceRoot: root, subagentDepth: 0}, svc: agentServices{writeScheduler: sched}}
	inner := &recordingWriter{name: "write_file", writesPaths: true}
	release, err := a.reserveParentWrite(inner, mustJSON(t, map[string]string{
		"path": filepath.Join(root, "a.md"), "content": "x",
	}), false, nil)
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

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type recordingWriter struct {
	name        string
	readOnly    bool
	calls       int
	writesPaths bool
}

type parentClaimProbeHooks struct {
	scheduler  *writeclaim.SubagentScheduler
	claim      writeclaim.WritePathSet
	acquireErr error
}

func (h *parentClaimProbeHooks) PreToolUse(context.Context, string, json.RawMessage) (bool, string) {
	release, err := h.scheduler.Acquire(context.Background(), writeclaim.AcquireRequest{
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
func (*parentClaimProbeHooks) HasPostLLMCall() bool                                   { return false }
func (*parentClaimProbeHooks) SubagentStart(context.Context, string, json.RawMessage) {}
func (*parentClaimProbeHooks) SubagentStop(context.Context, string, string, error)    {}
func (*parentClaimProbeHooks) PreCompact(context.Context, string) string              { return "" }

func (r *recordingWriter) Name() string        { return r.name }
func (r *recordingWriter) Description() string { return r.name }
func (r *recordingWriter) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}
func (r *recordingWriter) ReadOnly() bool         { return r.readOnly }
func (r *recordingWriter) WritesNamedPaths() bool { return r.writesPaths }
func (r *recordingWriter) Execute(context.Context, json.RawMessage) (string, error) {
	r.calls++
	return "ok", nil
}
