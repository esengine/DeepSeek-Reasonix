package main

import (
	"context"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestDesktopBranchTransitionMovesLeaseAndTabAtomically(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := sess.SaveIfAbsent(originalPath); err != nil {
		t.Fatal(err)
	}
	ag := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	app := &App{
		ctx:              context.Background(),
		tabs:             map[string]*WorkspaceTab{},
		detachedSessions: map[string]*WorkspaceTab{},
	}
	tab := &WorkspaceTab{ID: "tab", SessionPath: originalPath, Ready: true}
	ctrl := control.New(control.Options{
		Runner:              ag,
		Executor:            ag,
		SessionDir:          dir,
		SessionPath:         originalPath,
		Sink:                event.Discard,
		OnSessionTransition: app.handleTabSessionTransition(tab),
	})
	tab.Ctrl = ctrl
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.mu.Lock()
	app.newSessionRuntimeLocked(tab, sessionRuntimeKey(originalPath))
	app.mu.Unlock()
	if err := tab.ensureSessionLease(originalPath); err != nil {
		t.Fatal(err)
	}
	defer tab.releaseSessionLease()
	if err := bindTabWriteAuthority(tab, ctrl); err != nil {
		t.Fatal(err)
	}

	branchPath, err := ctrl.Branch("child")
	if err != nil {
		t.Fatalf("Branch: %v", err)
	}
	if tab.SessionPath != branchPath || ctrl.SessionPath() != branchPath {
		t.Fatalf("tab/controller paths = %q/%q, want %q", tab.SessionPath, ctrl.SessionPath(), branchPath)
	}
	tab.sessionLeaseMu.Lock()
	lease := tab.sessionLease
	tab.sessionLeaseMu.Unlock()
	if lease == nil || lease.Path() != agent.CanonicalSessionPath(branchPath) {
		t.Fatalf("tab lease = %v, want branch path", lease)
	}
	if auth := ag.Session().WriteAuthority(); auth == nil || !auth.Covers(branchPath) {
		t.Fatal("branch Session was published without target authority")
	}
	old, err := agent.TryAcquireSessionLease(originalPath)
	if err != nil {
		t.Fatalf("source lease remained held: %v", err)
	}
	old.Release()
	app.mu.Lock()
	runtime := app.runtimeBySessionKey[sessionRuntimeKey(branchPath)]
	app.mu.Unlock()
	if runtime == nil || runtime.Owner != tab {
		t.Fatal("runtime registry did not move to branch path")
	}
}
