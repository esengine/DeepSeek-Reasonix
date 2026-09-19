package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

// Exercise the same exact-identity endpoint as Desktop, including a replay
// while the first write is waiting and a subsequent write to the same root.
func TestWriteAccessExactApprovalAcrossConsecutiveWrites(t *testing.T) {
	for _, sessionScope := range []bool{false, true} {
		name := "once"
		if sessionScope {
			name = "session"
		}
		t.Run(name, func(t *testing.T) {
			workspace, outside := canonicalWriteTestDir(t), canonicalWriteTestDir(t)
			requests := make(chan event.Event, 8)
			results := make(chan agent.WriteAccessDecision, 2)
			finished := make(chan error, 1)
			c := newOwnedTestController(t, Options{
				WorkspaceRoot: workspace, WriteRoots: sandbox.NewWritableRootSet([]string{workspace}),
				RuntimeGeneration: 1, Policy: permission.New("allow", nil, nil, nil),
				Sink: event.FuncSink(func(e event.Event) {
					if e.Kind == event.ApprovalRequest {
						requests <- e
					}
				}),
			})
			c.EnableInteractiveApproval()
			c.SetToolApprovalMode(ToolApprovalWorkspaceWrite)
			c.SetTurnEventRoutingMetadata("write-access-runtime", "")
			t.Cleanup(c.Close)
			c.runGuarded(func(ctx context.Context) error {
				for range 2 {
					decision, err := c.CheckWriteAccess(ctx, agent.WriteAccessCheck{
						Tool: "write_file", Subject: filepath.Join(outside, "animation.html"), Expandable: true,
						Args:        json.RawMessage(`{"content":"fixture"}`),
						Declaration: tool.WriteAccessDeclaration{Directories: []string{outside}},
					})
					if err != nil {
						finished <- err
						return err
					}
					results <- decision
				}
				finished <- nil
				return nil
			})
			resolve := func(request event.Event) {
				t.Helper()
				answer := PromptAnswer{Allow: true, Session: sessionScope,
					Generation: request.Approval.Generation, PermissionRevision: request.Approval.PermissionRevision}
				identity := PromptIdentity{PromptID: request.Approval.ID, TurnID: request.TurnID,
					RuntimeEpoch: "write-access-runtime", Kind: PromptApproval}
				if err := c.ResolvePromptExact(identity, answer); err != nil {
					t.Fatalf("exact approval: %v", err)
				}
			}
			first := awaitPromptLedgerTest(t, requests, "first write approval")
			c.ReplayPendingPrompts()
			replay := awaitPromptLedgerTest(t, requests, "replayed write approval")
			if replay.Approval.ID != first.Approval.ID || replay.TurnID != first.TurnID {
				t.Fatal("replay changed the pending write identity")
			}
			resolve(replay)
			if got := awaitPromptLedgerTest(t, results, "first allowed write"); !got.Allow {
				t.Fatal("first write denied")
			}
			if !sessionScope {
				second := awaitPromptLedgerTest(t, requests, "second write approval")
				if second.Approval.ID == first.Approval.ID {
					t.Fatal("consecutive writes reused a prompt id")
				}
				resolve(second)
			}
			select {
			case got := <-results:
				if !got.Allow {
					t.Fatal("second write denied")
				}
			case unexpected := <-requests:
				t.Fatalf("session-scoped directory prompted again: %s", unexpected.Approval.ID)
			case <-time.After(5 * time.Second):
				t.Fatal("second write did not resume")
			}
			if err := awaitPromptLedgerTest(t, finished, "write completion"); err != nil {
				t.Fatal(err)
			}
			waitIdle(t, c)
		})
	}
}

func TestResolveApprovalWriteAccessOnceDoesNotGrantSession(t *testing.T) {
	dir := t.TempDir()
	outside := canonicalWriteTestDir(t)
	set := sandbox.NewWritableRootSet([]string{dir})
	c := newOwnedTestController(t, Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
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
	c := newOwnedTestController(t, Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
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
	c := newOwnedTestController(t, Options{
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
	c := newOwnedTestController(t, Options{
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
	c := newOwnedTestController(t, Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
	set.GrantSession([]string{extra})
	auth := c.SessionAuthorizations()
	if len(auth.WriteRoots) != 1 {
		t.Fatalf("WriteRoots = %v", auth.WriteRoots)
	}
	freshSet := sandbox.NewWritableRootSet([]string{dir})
	fresh := newOwnedTestController(t, Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: freshSet})
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
	c := newOwnedTestController(t, Options{Executor: exec, Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
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
	c := newOwnedTestController(t, Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
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
	c := newOwnedTestController(t, Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
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
	c := newOwnedTestController(t, Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
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
	c := newOwnedTestController(t, Options{
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
	c := newOwnedTestController(t, Options{Policy: permission.New("allow", nil, nil, nil), WriteRoots: set})
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
