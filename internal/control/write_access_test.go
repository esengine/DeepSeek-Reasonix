package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

func TestResolveApprovalWriteAccessOnceDoesNotGrantSession(t *testing.T) {
	dir := t.TempDir()
	outside := canonicalWriteTestDir(t)
	set := sandbox.NewWritableRootSet([]string{dir})
	c := New(Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
	id, reply := c.approval.registerWriteAccess("write_file", outside, "test", json.RawMessage(`{}`), &event.WriteAccessApproval{
		Directories:        []string{outside},
		DisplayDirectories: []string{"out"},
	})
	if err := c.ResolveApproval(id, true, sandbox.ApprovalScopeOnce); err != nil {
		t.Fatal(err)
	}
	got := <-reply
	if !got.allow || got.session || len(got.onceDirs) != 1 {
		t.Fatalf("once reply = %+v", got)
	}
	if set.Covers(outside) {
		t.Fatal("once grant must not enter the session set")
	}
}

func TestResolveApprovalWriteAccessSessionPersistsInSet(t *testing.T) {
	dir := t.TempDir()
	extra := canonicalWriteTestDir(t)
	set := sandbox.NewWritableRootSet([]string{dir})
	c := New(Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
	id, reply := c.approval.registerWriteAccess("write_file", extra, "test", json.RawMessage(`{}`), &event.WriteAccessApproval{
		Directories: []string{extra},
	})
	if err := c.ResolveApproval(id, true, sandbox.ApprovalScopeSession); err != nil {
		t.Fatal(err)
	}
	got := <-reply
	if !got.allow || !got.session {
		t.Fatalf("session reply = %+v", got)
	}
	if !set.Covers(extra) {
		t.Fatal("session grant should cover the directory")
	}
}

func TestResolveApprovalWriteAccessProjectScopeIsRejected(t *testing.T) {
	dir := t.TempDir()
	extra := canonicalWriteTestDir(t)
	set := sandbox.NewWritableRootSet([]string{dir})
	c := New(Options{
		Policy:               permission.New("allow", nil, nil, nil),
		WriteRoots:           set,
		OnPersistWriteAccess: func(dirs []string, permRule string) error { return errors.New("must not be called") },
	})
	id, reply := c.approval.registerWriteAccess("write_file", extra, "test", json.RawMessage(`{}`), &event.WriteAccessApproval{
		Directories: []string{extra},
	})
	if err := c.ResolveApproval(id, true, sandbox.ApprovalScopeProject); err == nil {
		t.Fatal("expected permanent scope rejection")
	}
	if set.Covers(extra) {
		t.Fatal("rejected permanent scope must not grant access")
	}
	select {
	case got := <-reply:
		t.Fatalf("rejected scope must keep the request pending, got %+v", got)
	default:
	}
}

func TestDangerFullAccessRetryRequiresRealExactDenialAndCanGrantSession(t *testing.T) {
	command := "installer --write-protected-state"
	denialID := sandbox.IssueDenial(command, "workspace-write")
	approvals := make(chan event.Approval, 1)
	c := New(Options{
		Policy:            permission.New("allow", nil, nil, nil),
		WriteRoots:        sandbox.NewWritableRootSet([]string{t.TempDir()}),
		RuntimeGeneration: 1,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvals <- e.Approval
			}
		}),
	})
	c.writeAccess.interactive = true
	request := func(id, cmd string) (agent.WriteAccessDecision, error) {
		args, _ := json.Marshal(map[string]string{"command": cmd, "sandbox_permissions": "danger-full-access", "justification": "complete the requested install", "denial_id": id})
		return c.CheckWriteAccess(context.Background(), agent.WriteAccessCheck{
			Tool: "bash", Subject: cmd, Args: args, Expandable: true,
			Declaration: tool.WriteAccessDeclaration{RequestedPreset: "danger-full-access", Justification: "complete the requested install", DenialID: id},
		})
	}
	result := make(chan agent.WriteAccessDecision, 1)
	go func() {
		decision, _ := request(denialID, command)
		result <- decision
	}()
	approval := <-approvals
	if approval.Generation == 0 || approval.PermissionRevision == 0 {
		t.Fatalf("approval lacks runtime identity: %+v", approval)
	}
	if err := c.ResolveApprovalAt(approval.ID, true, sandbox.ApprovalScopeSession, approval.Generation, approval.PermissionRevision); err != nil {
		t.Fatal(err)
	}
	if decision := <-result; !decision.Allow || decision.PermissionPreset != "danger-full-access" {
		t.Fatalf("authorized retry = %+v", decision)
	}
	decision, err := request("", command)
	if err != nil || !decision.Allow || decision.PermissionPreset != "danger-full-access" {
		t.Fatalf("session-scoped exact retry = (%+v, %v)", decision, err)
	}
	decision, err = request("", command+" --other")
	if err != nil || decision.Allow || !strings.Contains(decision.Reason, "denial_id") {
		t.Fatalf("different command retry = (%+v, %v)", decision, err)
	}
}

func TestSessionAuthorizationsCarryWriteRoots(t *testing.T) {
	dir := t.TempDir()
	extra := t.TempDir()
	set := sandbox.NewWritableRootSet([]string{dir})
	c := New(Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
	set.GrantSession([]string{extra})
	auth := c.SessionAuthorizations()
	if len(auth.WriteRoots) != 1 {
		t.Fatalf("WriteRoots = %v", auth.WriteRoots)
	}
	freshSet := sandbox.NewWritableRootSet([]string{dir})
	fresh := New(Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: freshSet})
	fresh.RestoreSessionAuthorizations(auth)
	if !freshSet.Covers(extra) {
		t.Fatal("rebuild must restore session write roots")
	}
}

func TestNewSessionClearsWriteRoots(t *testing.T) {
	dir := t.TempDir()
	extra := t.TempDir()
	set := sandbox.NewWritableRootSet([]string{dir})
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
	set.GrantSession([]string{extra})
	if err := c.NewSession(); err != nil {
		t.Fatal(err)
	}
	if set.Covers(extra) {
		t.Fatal("/new must clear session write roots")
	}
}

func TestCheckWriteAccessHeadlessMissingDir(t *testing.T) {
	dir := t.TempDir()
	set := sandbox.NewWritableRootSet([]string{dir})
	c := New(Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
	dec, err := c.CheckWriteAccess(context.Background(), agent.WriteAccessCheck{
		Tool:       "write_file",
		Expandable: true,
		Declaration: tool.WriteAccessDeclaration{
			Directories: []string{filepath.Join(os.TempDir(), "reasonix-write-access-outside")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allow {
		t.Fatal("headless must not grant a new directory")
	}
	if dec.Reason == "" {
		t.Fatal("expected --add-dir guidance")
	}
}

func TestCheckWriteAccessSubagentCannotExpand(t *testing.T) {
	dir := t.TempDir()
	set := sandbox.NewWritableRootSet([]string{dir})
	c := New(Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
	c.writeAccess.interactive = true
	dec, err := c.CheckWriteAccess(context.Background(), agent.WriteAccessCheck{
		Tool:       "write_file",
		Expandable: false,
		Declaration: tool.WriteAccessDeclaration{
			Directories: []string{filepath.Join(os.TempDir(), "reasonix-write-access-child")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allow {
		t.Fatal("sub-agent must not expand write access")
	}
}

func TestWriteAccessNotDrainedByAutoOrYolo(t *testing.T) {
	dir := t.TempDir()
	extra := t.TempDir()
	set := sandbox.NewWritableRootSet([]string{dir})
	c := New(Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
	id, reply := c.approval.registerWriteAccess("bash", extra, "test", json.RawMessage(`{}`), &event.WriteAccessApproval{
		Directories: []string{extra},
	})
	if drained := c.approval.setMode(ToolApprovalAuto); len(drained) != 0 {
		t.Fatalf("Auto drained write-access: %+v", drained)
	}
	if drained := c.approval.setMode(ToolApprovalYolo); len(drained) != 0 {
		t.Fatalf("YOLO drained write-access: %+v", drained)
	}
	pending := c.approval.peek(id)
	if pending.reply == nil {
		t.Fatal("write-access approval must stay pending")
	}
	pending = c.approval.resolve(id)
	pending.reply <- approvalReply{}
	<-reply
}

func TestCheckWriteAccessDenyBeatsDirectoryPrompt(t *testing.T) {
	dir := t.TempDir()
	set := sandbox.NewWritableRootSet([]string{dir})
	c := New(Options{
		Policy:     permission.New("ask", nil, nil, []string{"write_file"}),
		WriteRoots: set,
	})
	c.writeAccess.interactive = true
	dec, err := c.CheckWriteAccess(context.Background(), agent.WriteAccessCheck{
		Tool:       "write_file",
		Expandable: true,
		Declaration: tool.WriteAccessDeclaration{
			Directories: []string{t.TempDir()},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allow {
		t.Fatal("explicit deny must not show a directory approval")
	}
	if !strings.Contains(dec.Reason, "deny") {
		t.Fatalf("reason = %q", dec.Reason)
	}
}

func TestCheckWriteAccessBashWithoutSandboxSkips(t *testing.T) {
	dir := t.TempDir()
	set := sandbox.NewWritableRootSet([]string{dir})
	c := New(Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
	c.writeAccess.interactive = true
	dec, err := c.CheckWriteAccess(context.Background(), agent.WriteAccessCheck{
		Tool:       "bash",
		Expandable: true,
		Declaration: tool.WriteAccessDeclaration{
			Directories:   []string{t.TempDir()},
			Justification: "install",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allow {
		t.Fatalf("unenforced bash must keep existing platform behavior, got %+v", dec)
	}
}

func canonicalWriteTestDir(t *testing.T) string {
	t.Helper()
	dir, err := sandbox.ResolveAbsPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
