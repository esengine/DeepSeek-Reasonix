package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

// TestSmartCloseDecision covers every branch of the pure decision policy on
// every platform (no skips): idle quits, active work with a restore path
// backgrounds, active work without a restore path stays visible.
func TestSmartCloseDecision(t *testing.T) {
	cases := []struct {
		activeWork       int
		restoreAvailable bool
		want             smartCloseAction
	}{
		{0, false, smartCloseQuit},
		{0, true, smartCloseQuit},
		{1, false, smartCloseStayVisible},
		{3, false, smartCloseStayVisible},
		{1, true, smartCloseBackground},
		{2, true, smartCloseBackground},
	}
	for _, tc := range cases {
		got := smartCloseDecision(tc.activeWork, tc.restoreAvailable)
		if got != tc.want {
			t.Errorf("smartCloseDecision(%d, %v) = %v, want %v", tc.activeWork, tc.restoreAvailable, got, tc.want)
		}
	}
}

// TestSmartCloseActiveWorkPreventsClose verifies the Wails contract wiring
// with a real active-work signal (injected counter): whenever active work
// exists, smartClose must prevent the close (return true) — both when it
// backgrounds and when it stays visible. This runs on every platform.
func TestSmartCloseActiveWorkPreventsClose(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	app := NewApp()
	app.runtimeWorkCounter = func() int { return 2 } // two active runtimes
	if !app.smartClose(context.Background()) {
		t.Fatal("smartClose must prevent the close (return true) whenever active work exists")
	}
}

// TestSmartCloseStayVisibleUsesActiveWorkBranch verifies the no-restore-path
// decision is exercised through the real integration path on platforms where
// restore is unavailable.
func TestSmartCloseStayVisibleUsesActiveWorkBranch(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	app := NewApp()
	app.runtimeWorkCounter = func() int { return 1 }
	if !app.backgroundCloseHasRestorePath() {
		if !app.smartClose(context.Background()) {
			t.Fatal("smartClose must prevent the close when active work has no restore path")
		}
	}
}

func TestSmartCloseQuitsWhenIdle(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	app := NewApp()
	// No tabs, no controllers: nothing active, so the close is allowed.
	if app.smartClose(context.Background()) {
		t.Fatal("smartClose must allow the close (return false) when idle")
	}
}

func TestReopenSessionCopyCreatesRealCopy(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	app := NewApp()
	root := t.TempDir()
	dir := filepath.Join(root, ".reasonix", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(dir, "20260101-000000.000000000-session.jsonl")
	session := &agent.Session{Messages: []provider.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "hello"},
	}}
	if err := session.Save(original); err != nil {
		t.Fatal(err)
	}
	if err := pinSessionBranchMeta(original, "project", root, "topic-test", "Copy test"); err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{ID: "tab-test", Scope: "project", WorkspaceRoot: root, TopicID: "topic-test", SessionPath: original}
	app.mu.Lock()
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.mu.Unlock()

	if err := app.reopenSessionCopy(tab, original, ""); err != nil {
		t.Fatal(err)
	}
	// reopenSessionCopy rebuilds the controller asynchronously. Wait for that
	// build to publish either a controller or a startup error, then close the
	// runtime and release its lease before TempDir cleanup. Without this
	// synchronization Windows can race RemoveAll against the build's sidecar
	// writes and report "The directory is not empty" after all assertions pass.
	deadline := time.Now().Add(30 * time.Second)
	for {
		app.mu.RLock()
		ctrl := tab.Ctrl
		startupErr := tab.StartupErr
		app.mu.RUnlock()
		if ctrl != nil || startupErr != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("copied session controller rebuild did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	t.Cleanup(func() {
		app.mu.RLock()
		ctrl := tab.Ctrl
		app.mu.RUnlock()
		if ctrl != nil {
			ctrl.Close()
		}
		tab.releaseSessionLease()
	})
	app.mu.RLock()
	copied := tab.SessionPath
	app.mu.RUnlock()
	if copied == original {
		t.Fatal("copy action did not change the session path")
	}
	if filepath.Dir(copied) != desktopSessionDir(root) {
		t.Errorf("copy path %q is not inside the session dir %q", copied, desktopSessionDir(root))
	}
	cloned, err := agent.LoadSession(copied)
	if err != nil {
		t.Fatalf("cloned session does not load: %v", err)
	}
	if len(cloned.Messages) != 2 || cloned.Messages[1].Content != "hello" {
		t.Errorf("cloned content = %+v, want the original messages", cloned.Messages)
	}
	// The original stays untouched.
	orig, err := agent.LoadSession(original)
	if err != nil || len(orig.Messages) != 2 || orig.Messages[1].Content != "hello" {
		t.Errorf("original modified by copy: %v (%v)", orig.Messages, err)
	}
}

func TestResolveSessionIssueRejectsStaleAndUnownedActions(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	app := NewApp()
	root := t.TempDir()
	dir := filepath.Join(root, ".reasonix", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "20260101-000000.000000000-session.jsonl")
	if err := os.WriteFile(path, []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{ID: "tab-test", WorkspaceRoot: root, SessionPath: path}
	app.mu.Lock()
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	rt := app.newSessionRuntimeLocked(tab, sessionRuntimeKey(path))
	rt.Phase = sessionRuntimeLeaseBlocked
	rt.Issue = &SessionRuntimeIssue{
		Code:      "session_lease_held",
		IssueID:   "issue-1",
		Message:   "held elsewhere",
		Retryable: true,
		OwnerKind: sessionOwnerExternal,
		Actions:   []string{"retry", "read_only", "copy"},
		epoch:     rt.Epoch,
	}
	app.mu.Unlock()

	cases := []struct {
		issueID string
		action  string
		wantErr string
	}{
		{"wrong-id", "retry", "no longer current"},
		{"", "retry", "no longer current"},
		{"issue-1", "focus", "not allowed"}, // focus is not advertised for external_process
		{"issue-1", "delete", "not allowed"},
	}
	for _, tc := range cases {
		err := app.ResolveSessionRuntimeIssue(tab.ID, tc.issueID, tc.action)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("Resolve(%q, %q) err = %v, want containing %q", tc.issueID, tc.action, err, tc.wantErr)
		}
	}
}

func TestResolveSessionIssueRejectsAdvancedEpoch(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	app := NewApp()
	root := t.TempDir()
	dir := filepath.Join(root, ".reasonix", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "20260101-000000.000000000-session.jsonl")
	if err := os.WriteFile(path, []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{ID: "tab-test", WorkspaceRoot: root, SessionPath: path}
	app.mu.Lock()
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	rt := app.newSessionRuntimeLocked(tab, sessionRuntimeKey(path))
	rt.Phase = sessionRuntimeLeaseBlocked
	rt.Issue = &SessionRuntimeIssue{
		Code:      "session_lease_held",
		IssueID:   "issue-1",
		Message:   "held elsewhere",
		Retryable: true,
		OwnerKind: sessionOwnerStale,
		Actions:   []string{"retry"},
		epoch:     rt.Epoch,
	}
	// The runtime advances (a rebuild happened) after the issue was raised.
	rt.Epoch = newSessionRuntimeID("epoch")
	app.mu.Unlock()

	err := app.ResolveSessionRuntimeIssue(tab.ID, "issue-1", "retry")
	if err == nil || !strings.Contains(err.Error(), "advanced") {
		t.Errorf("stale-epoch action accepted: %v", err)
	}
}

func TestStartupFailedIssueActionsNonNull(t *testing.T) {
	issue := sessionRuntimeIssueForError(errors.New("boom"))
	if issue.Actions == nil {
		t.Fatal("startup_failed issue must carry a non-null actions array")
	}
}

func TestClassifySessionOwnerDetached(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	kind, _, _, _, _ := classifySessionOwner(path, nil, true)
	if kind != sessionOwnerCurrentDetached {
		t.Fatalf("kind = %s, want current_detached", kind)
	}
}
