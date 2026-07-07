package main

import (
	"context"
	"testing"
)

func sharedHostRefsForTest(t *testing.T, app *App, root string) (int, bool) {
	t.Helper()
	app.sharedHostsMu.Lock()
	defer app.sharedHostsMu.Unlock()
	entry, ok := app.sharedHosts[root]
	if !ok {
		return 0, false
	}
	return entry.refs, true
}

func TestCloneDetachedRuntimeTabPreservesSharedHostKey(t *testing.T) {
	tab := &WorkspaceTab{ID: "tab", SharedHostKey: "workspace-root"}
	detached := cloneDetachedRuntimeTab(tab, "session-key", "session-key")
	if detached == nil {
		t.Fatal("cloneDetachedRuntimeTab returned nil")
	}
	if detached.SharedHostKey != tab.SharedHostKey {
		t.Fatalf("detached SharedHostKey = %q, want %q", detached.SharedHostKey, tab.SharedHostKey)
	}
}

func TestApplyRuntimeTabTransfersDetachedSharedHostKey(t *testing.T) {
	target := &WorkspaceTab{ID: "visible", SharedHostKey: "new-build-ref"}
	source := &WorkspaceTab{ID: "detached", SharedHostKey: "detached-ref"}

	applyRuntimeTab(target, source, "session-key", context.Background(), NewApp())

	if target.SharedHostKey != "detached-ref" {
		t.Fatalf("target SharedHostKey = %q, want detached-ref", target.SharedHostKey)
	}
}

func TestCloseRemovedSessionRuntimesReleasesSharedHost(t *testing.T) {
	app := NewApp()
	root := "workspace-root"
	app.acquireSharedHost(root)

	tab := &WorkspaceTab{ID: "tab", SharedHostKey: root}
	app.closeRemovedSessionRuntimes([]removedSessionRuntime{{tab: tab}})

	if tab.SharedHostKey != "" {
		t.Fatalf("tab SharedHostKey = %q, want cleared", tab.SharedHostKey)
	}
	if refs, ok := sharedHostRefsForTest(t, app, root); ok {
		t.Fatalf("shared host still present with refs=%d, want released", refs)
	}
}
