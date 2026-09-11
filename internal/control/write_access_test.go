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

func TestResolveApprovalWriteAccessProjectFailureDoesNotGrant(t *testing.T) {
	dir := t.TempDir()
	extra := canonicalWriteTestDir(t)
	set := sandbox.NewWritableRootSet([]string{dir})
	c := New(Options{
		Policy:     permission.New("allow", nil, nil, nil),
		WriteRoots: set,
		OnPersistWriteAccess: func(dirs []string, permRule string) error {
			return errors.New("disk locked")
		},
	})
	id, reply := c.approval.registerWriteAccess("write_file", extra, "test", json.RawMessage(`{}`), &event.WriteAccessApproval{
		Directories: []string{extra},
	})
	if err := c.ResolveApproval(id, true, sandbox.ApprovalScopeProject); err == nil {
		t.Fatal("expected persist error")
	}
	got := <-reply
	if got.allow || got.persistErr == nil {
		t.Fatalf("failed persist must deny, got %+v", got)
	}
	if set.Covers(extra) {
		t.Fatal("failed persist must not grant session access")
	}
}

func TestResolveApprovalWriteAccessProjectSurvivesNewSession(t *testing.T) {
	base := t.TempDir()
	extra := canonicalWriteTestDir(t)
	set := sandbox.NewWritableRootSet([]string{base})
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	var persisted []string
	c := New(Options{
		Executor:   exec,
		Policy:     permission.New("allow", nil, nil, nil),
		WriteRoots: set,
		OnPersistWriteAccess: func(dirs []string, _ string) error {
			persisted = append([]string(nil), dirs...)
			return nil
		},
	})
	c.SetSessionPath(agent.NewSessionPath(t.TempDir(), "test"))
	id, reply := c.approval.registerWriteAccess("write_file", extra, "test", json.RawMessage(`{}`), &event.WriteAccessApproval{
		Directories: []string{extra},
	})
	if err := c.ResolveApproval(id, true, sandbox.ApprovalScopeProject); err != nil {
		t.Fatal(err)
	}
	got := <-reply
	if !got.allow || !got.persist || len(persisted) != 1 || persisted[0] != extra {
		t.Fatalf("project reply = %+v, persisted = %v", got, persisted)
	}
	if err := c.NewSession(); err != nil {
		t.Fatal(err)
	}
	if !set.Covers(extra) {
		t.Fatal("project write grant must survive /new as a baseline root")
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
