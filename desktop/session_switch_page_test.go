package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// appendHistoryTestSessionTurn writes one more turn to a session's durable log
// while a switch into that session is in flight. A switch that re-reads the log
// to build its first screen surfaces the appended turn; one that reuses the
// transcript it already loaded for the rebind does not.
func appendHistoryTestSessionTurn(t *testing.T, path, prompt string) {
	t.Helper()
	session, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	session.Add(provider.Message{Role: provider.RoleUser, Content: prompt})
	if err := session.Save(path); err != nil {
		t.Fatalf("save %s: %v", path, err)
	}
}

// appendOnCommit makes the durable log grow once the switch has already loaded
// it, so a page built from a second read would show the extra turn.
func appendOnCommit(t *testing.T, app *App, path, prompt string) {
	t.Helper()
	app.rebindCandidateHook = func(stage string) error {
		if stage == "committed" {
			appendHistoryTestSessionTurn(t, path, prompt)
		}
		return nil
	}
}

func requireSingleDurableRead(t *testing.T, page HistoryPage) {
	t.Helper()
	if page.Switch == nil {
		t.Fatal("switch page carries no phase breakdown")
	}
	if page.Switch.Outcome != "ok" {
		t.Fatalf("switch outcome = %q, want ok", page.Switch.Outcome)
	}
	if page.Switch.DurableReads != 1 {
		t.Fatalf("switch durable reads = %d, want 1", page.Switch.DurableReads)
	}
	if page.Switch.LoadedCount == 0 || page.Switch.HistoryCount == 0 {
		t.Fatalf("switch phase counts missing: %+v", page.Switch)
	}
}

func requireHistoryPagesMatch(t *testing.T, want, got HistoryPage, label string) {
	t.Helper()
	if got.StartTurn != want.StartTurn || got.EndTurn != want.EndTurn ||
		got.TotalTurns != want.TotalTurns || got.HasOlder != want.HasOlder {
		t.Fatalf("%s window = %d-%d/%d older=%v, want %d-%d/%d older=%v", label,
			got.StartTurn, got.EndTurn, got.TotalTurns, got.HasOlder,
			want.StartTurn, want.EndTurn, want.TotalTurns, want.HasOlder)
	}
	if got.Digest != want.Digest {
		t.Fatalf("%s digest = %q, want %q", label, got.Digest, want.Digest)
	}
	if got.Revision != want.Revision {
		t.Fatalf("%s revision = %d, want %d", label, got.Revision, want.Revision)
	}
	wantJSON, err := json.Marshal(want.Messages)
	if err != nil {
		t.Fatalf("marshal want messages: %v", err)
	}
	gotJSON, err := json.Marshal(got.Messages)
	if err != nil {
		t.Fatalf("marshal got messages: %v", err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("%s messages differ:\n got %s\nwant %s", label, gotJSON, wantJSON)
	}
}

// resumedCleanTarget writes a target session whose persisted system prompt is
// already the composed one. The rebound controller is then content-identical to
// the log, so a switch that re-reads the log is the only way the page can pick
// up a write that lands after the load.
func resumedCleanTarget(t *testing.T, app *App, tab *WorkspaceTab, name string) string {
	t.Helper()
	dir := filepath.Dir(tab.currentSessionPath())
	probe := filepath.Join(dir, name+".probe.jsonl")
	writeHistoryTestSession(t, probe, "probe prompt")
	if _, err := app.ResumeSessionPageForTab(tab.ID, probe, defaultHistoryPageTurns); err != nil {
		t.Fatalf("probe switch: %v", err)
	}
	prompt := systemPromptFrom(app.controllerForTab(tab).History())
	if strings.TrimSpace(prompt) == "" {
		t.Fatal("rebuilt controller composed no system prompt")
	}
	path := filepath.Join(dir, name)
	session := agent.NewSession(prompt)
	session.Add(provider.Message{Role: provider.RoleUser, Content: "target prompt"})
	if err := session.Save(path); err != nil {
		t.Fatalf("save %s: %v", path, err)
	}
	return path
}

func requireHistoryTurnCount(t *testing.T, page HistoryPage, want int, label string) {
	t.Helper()
	if page.TotalTurns != want {
		t.Fatalf("%s totalTurns = %d, want %d", label, page.TotalTurns, want)
	}
}

func TestResumeSessionPageBuildsFirstScreenFromOneDurableRead(t *testing.T) {
	app, tab, _, _, _, _ := newAtomicRebindTestApp(t)
	targetPath := resumedCleanTarget(t, app, tab, "resume-once-target.jsonl")
	appendOnCommit(t, app, targetPath, "appended mid-switch")

	page, err := app.ResumeSessionPageForTab(tab.ID, targetPath, defaultHistoryPageTurns)
	if err != nil {
		t.Fatalf("ResumeSessionPageForTab: %v", err)
	}
	requireSingleDurableRead(t, page)
	requireHistoryTurnCount(t, page, 1, "switch page")
	// Control: the write is real, and a page that does read the log sees it.
	requireHistoryTurnCount(t, app.HistoryPageForTab(tab.ID, 0, defaultHistoryPageTurns), 2, "durable page")
}

func TestOpenChannelSessionPageBuildsFirstScreenFromOneDurableRead(t *testing.T) {
	app, tab, _, _, _, _ := newAtomicRebindTestApp(t)
	targetPath := resumedCleanTarget(t, app, tab, "channel-once-target.jsonl")
	appendOnCommit(t, app, targetPath, "appended mid-switch")

	page, err := app.OpenChannelSessionPageForTab(tab.ID, targetPath, defaultHistoryPageTurns)
	if err != nil {
		t.Fatalf("OpenChannelSessionPageForTab: %v", err)
	}
	requireSingleDurableRead(t, page)
	requireHistoryTurnCount(t, page, 1, "channel switch page")
	if !tab.ReadOnly {
		t.Fatal("channel switch must leave the tab read-only")
	}
}

func TestSwitchFirstScreenMatchesDurablePage(t *testing.T) {
	app, tab, _, _, targetPath, loaded := newAtomicRebindTestApp(t)

	page, err := app.ResumeSessionPageForTab(tab.ID, targetPath, defaultHistoryPageTurns)
	if err != nil {
		t.Fatalf("ResumeSessionPageForTab: %v", err)
	}
	durable := app.HistoryPageForTab(tab.ID, 0, defaultHistoryPageTurns)
	requireHistoryPagesMatch(t, durable, page, "switch page")

	// The same page built from the preloaded transcript must be byte-identical to
	// the one built by a real durable read, which is what makes the reuse safe.
	preloaded, readLog := historyPageForController(tab, app.controllerForTab(tab), loaded, targetPath, 0, defaultHistoryPageTurns)
	if readLog {
		t.Fatal("a preloaded transcript must satisfy the durable branch without another read")
	}
	requireHistoryPagesMatch(t, durable, preloaded, "preloaded page")
}

func TestSequentialSwitchesKeepPageIdentityWithTheirSession(t *testing.T) {
	app, tab, _, _, targetPath, _ := newAtomicRebindTestApp(t)
	dir := filepath.Dir(targetPath)
	thirdPath := filepath.Join(dir, "sequential-third.jsonl")
	writeHistoryTestSession(t, thirdPath, "third prompt")

	first, err := app.ResumeSessionPageForTab(tab.ID, targetPath, defaultHistoryPageTurns)
	if err != nil {
		t.Fatalf("switch to target: %v", err)
	}
	second, err := app.ResumeSessionPageForTab(tab.ID, thirdPath, defaultHistoryPageTurns)
	if err != nil {
		t.Fatalf("switch to third: %v", err)
	}
	if first.Digest == "" || second.Digest == "" || first.Digest == second.Digest {
		t.Fatalf("page digests = %q then %q, want distinct non-empty fingerprints", first.Digest, second.Digest)
	}
	if got := tab.currentSessionPath(); sessionRuntimeKey(got) != sessionRuntimeKey(thirdPath) {
		t.Fatalf("tab session after sequential switches = %q, want %q", got, thirdPath)
	}
	requireHistoryPagesMatch(t, app.HistoryPageForTab(tab.ID, 0, defaultHistoryPageTurns), second, "final page")
}

func TestResumeSessionPageKeepsUnsavedControllerTail(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(dir, "unsaved-tail.jsonl")
	writeHistoryTestSession(t, sessionPath, "durable prompt")

	session, err := agent.LoadSession(sessionPath)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	exec := agent.New(nil, nil, session, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{
		Executor: exec, SessionDir: dir, SessionPath: sessionPath, Label: "tail", Sink: event.Discard,
	})
	ctrl.Resume(session, sessionPath)
	// An in-memory turn with no save behind it: the durable log must not displace
	// it, which is the running-session side of the switch contract.
	session.Add(provider.Message{Role: provider.RoleUser, Content: "unsaved tail"})

	app := newRebindTestApp(t, root, sessionPath, ctrl, "unsaved-tail")
	page, err := app.ResumeSessionPageForTab("unsaved-tail", sessionPath, defaultHistoryPageTurns)
	if err != nil {
		t.Fatalf("ResumeSessionPageForTab: %v", err)
	}
	if page.TotalTurns != 2 {
		t.Fatalf("page totalTurns = %d, want 2: the durable log displaced the controller's unsaved tail", page.TotalTurns)
	}
}

func TestResumeSessionPageRebindFailureKeepsSourceRuntime(t *testing.T) {
	app, tab, oldCtrl, sourcePath, targetPath, _ := newAtomicRebindTestApp(t)
	app.mu.RLock()
	oldEpoch := app.sessionRuntimeViewLocked(tab).Epoch
	app.mu.RUnlock()

	holder, err := agent.TryAcquireSessionLease(targetPath)
	if err != nil {
		t.Fatalf("hold target lease: %v", err)
	}
	defer holder.Release()

	page, err := app.ResumeSessionPageForTab(tab.ID, targetPath, defaultHistoryPageTurns)
	if !errors.Is(err, agent.ErrSessionLeaseHeld) {
		t.Fatalf("switch error = %v, want ErrSessionLeaseHeld", err)
	}
	if len(page.Messages) != 0 || page.Switch != nil {
		t.Fatalf("failed switch returned a committed surface: %+v", page)
	}
	assertAtomicRebindFailurePreservedSource(t, app, tab, oldCtrl, sourcePath, targetPath, oldEpoch)
}

func TestResumeSessionPageFollowsCanonicalContinuation(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	question := provider.Message{Role: provider.RoleUser, Content: "question"}
	answer := provider.Message{Role: provider.RoleAssistant, Content: "answer"}
	next := provider.Message{Role: provider.RoleUser, Content: "next"}
	done := provider.Message{Role: provider.RoleAssistant, Content: "done"}
	save := func(path, topic string, messages ...provider.Message) {
		t.Helper()
		session := agent.NewSession("sys")
		for _, message := range messages {
			session.Add(message)
		}
		if err := session.Save(path); err != nil {
			t.Fatal(err)
		}
		if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
			ID: agent.BranchID(path), Scope: "global", TopicID: topic, TopicTitle: "Upgraded",
		}); err != nil {
			t.Fatal(err)
		}
	}
	parentPath := filepath.Join(dir, "continuation-parent.jsonl")
	leafPath := filepath.Join(dir, "continuation-leaf.jsonl")
	save(parentPath, "conversation", question, answer)
	save(leafPath, "legacy-leaf-topic", question, answer, next, done)
	if err := agent.SaveBranchMetaPreserveUpdated(leafPath, agent.BranchMeta{
		ID: agent.BranchID(leafPath), Scope: "global", TopicID: "legacy-leaf-topic",
		Recovered: true, ParentID: agent.BranchID(parentPath), RecoveryDepth: 1,
	}); err != nil {
		t.Fatal(err)
	}

	parent, err := agent.LoadSession(parentPath)
	if err != nil {
		t.Fatalf("load parent: %v", err)
	}
	exec := agent.New(nil, nil, parent, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{
		Executor: exec, SessionDir: dir, SessionPath: parentPath, Label: "parent", Sink: event.Discard,
	})
	ctrl.Resume(parent, parentPath)

	app := newRebindTestApp(t, root, parentPath, ctrl, "continuation")
	installSessionCatalogForTest(t, app, dir, "global", "")
	if got := app.continuePathForOpen(parentPath); got != leafPath {
		t.Fatalf("continuePathForOpen = %q, want covering leaf %q", got, leafPath)
	}

	page, err := app.ResumeSessionPageForTab("continuation", parentPath, defaultHistoryPageTurns)
	if err != nil {
		t.Fatalf("ResumeSessionPageForTab: %v", err)
	}
	requireSingleDurableRead(t, page)
	bound := app.controllerForTab(app.tabs["continuation"])
	if got := bound.SessionPath(); sessionRuntimeKey(got) != sessionRuntimeKey(leafPath) {
		t.Fatalf("bound path = %q, want continuation %q", got, leafPath)
	}
	// The window must be the leaf's two turns, not the parent's one.
	requireHistoryTurnCount(t, page, 2, "continuation page")
	// The fingerprint must describe the transcript the page displays: it is what
	// the next slice compares against to detect real drift.
	digest, err := agent.ContentDigestForMessages(bound.History())
	if err != nil {
		t.Fatalf("digest bound transcript: %v", err)
	}
	if page.Digest != digest {
		t.Fatalf("page digest = %q, want bound transcript digest %q", page.Digest, digest)
	}
	parentDigest, err := agent.ContentDigestForMessages(parent.Snapshot())
	if err != nil {
		t.Fatalf("digest parent: %v", err)
	}
	if page.Digest == parentDigest {
		t.Fatal("page fingerprint still names the pre-continuation parent")
	}
}

// newRebindTestApp builds the smallest App a switch needs: one global-scope tab
// with a live controller, its session lease, a published runtime, and a ready
// sink.
func newRebindTestApp(t *testing.T, root, sessionPath string, ctrl control.SessionAPI, tabID string) *App {
	t.Helper()
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	tab := &WorkspaceTab{
		ID:            tabID,
		Scope:         "global",
		WorkspaceRoot: root,
		SessionPath:   sessionPath,
		Ctrl:          ctrl,
		Ready:         true,
		sink:          &tabEventSink{tabID: tabID, app: app, ctx: app.ctx},
		disabledMCP:   map[string]ServerView{},
	}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	if err := tab.ensureSessionLease(sessionPath); err != nil {
		t.Fatalf("lease session: %v", err)
	}
	app.mu.Lock()
	app.newSessionRuntimeLocked(tab, sessionRuntimeKey(sessionPath))
	app.advanceSessionRuntimeEpochLocked(tab)
	app.mu.Unlock()
	t.Cleanup(func() {
		if live := app.controllerForTab(tab); live != nil {
			live.Close()
		}
		tab.releaseSessionLease()
	})
	return app
}
