package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncatalog"
)

func TestStartTopicActivationPrefersLiveRuntimeOverRepresentative(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	sessionDir := desktopSessionDir(projectRoot)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	oldPath := writeTopicSessionWithPrompt(t, sessionDir, "old.jsonl", "topic-b", "Topic B", projectRoot, "yesterday unsigned todos", time.Now().Add(-24*time.Hour))
	livePath := writeTopicSessionWithPrompt(t, sessionDir, "live.jsonl", "topic-b", "Topic B", projectRoot, "today live turn", time.Now())

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	events := newActivationEventRecorder(app)
	t.Cleanup(func() { app.shutdown(context.Background()) })

	stub := &activationStubController{sessionPath: livePath}
	live := &WorkspaceTab{
		ID:             "tab-live",
		Scope:          "project",
		WorkspaceRoot:  projectRoot,
		TopicID:        "topic-b",
		TopicTitle:     "Topic B",
		SessionPath:    livePath,
		Ctrl:           stub,
		Label:          "stub-model",
		Ready:          true,
		ActivityStatus: topicStatusPaused,
		disabledMCP:    map[string]ServerView{},
	}
	live.sink = &tabEventSink{tabID: live.ID, app: app}
	installNoopRuntimeEvents(app, live.sink)
	if err := live.ensureSessionLease(livePath); err != nil {
		t.Fatalf("ensureSessionLease live: %v", err)
	}
	app.detachedSessions[sessionRuntimeKey(livePath)] = live

	ticket, err := app.StartTopicActivation(TopicActivationRequest{
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       "topic-b",
		SessionPath:   oldPath,
		RequestID:     "req-live",
	})
	if err != nil {
		t.Fatalf("StartTopicActivation: %v", err)
	}
	events.waitFor(t, activationEventFor("req-live", "ready"))

	app.mu.RLock()
	tab := app.tabs[ticket.TabID]
	app.mu.RUnlock()
	if tab == nil {
		t.Fatal("activated tab missing")
	}
	if tab.Ctrl != stub {
		t.Fatal("clicking the live topic row opened the catalog representative instead of attaching the live controller")
	}
	if sessionRuntimeKey(tab.currentSessionPath()) != sessionRuntimeKey(livePath) {
		t.Fatalf("session path = %q, want live %q", tab.currentSessionPath(), livePath)
	}
	if stub.closed.Load() {
		t.Fatal("live controller was closed while attaching")
	}
	if ticket.Meta.SessionPath != "" && sessionRuntimeKey(ticket.Meta.SessionPath) != sessionRuntimeKey(livePath) {
		t.Fatalf("ticket session path = %q, want live %q", ticket.Meta.SessionPath, livePath)
	}
}

func TestStartTopicActivationReadyDoesNotWaitForRebuildMutex(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	sessionDir := desktopSessionDir(projectRoot)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	pathA := writeTopicSession(t, sessionDir, "a.jsonl", "topic-a", "Topic A", projectRoot)
	oldB := writeTopicSessionWithPrompt(t, sessionDir, "b-old.jsonl", "topic-b", "Topic B", projectRoot, "old b", time.Now().Add(-time.Hour))
	liveB := writeTopicSessionWithPrompt(t, sessionDir, "b-live.jsonl", "topic-b", "Topic B", projectRoot, "live b", time.Now())

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	events := newActivationEventRecorder(app)
	t.Cleanup(func() { app.shutdown(context.Background()) })

	stubA := &activationStubController{sessionPath: pathA}
	tabA := &WorkspaceTab{
		ID:            "tab-a",
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       "topic-a",
		TopicTitle:    "Topic A",
		SessionPath:   pathA,
		Ctrl:          stubA,
		Label:         "stub-a",
		Ready:         true,
		disabledMCP:   map[string]ServerView{},
	}
	tabA.sink = &tabEventSink{tabID: tabA.ID, app: app}
	installNoopRuntimeEvents(app, tabA.sink)
	if err := tabA.ensureSessionLease(pathA); err != nil {
		t.Fatalf("ensureSessionLease A: %v", err)
	}
	app.tabs[tabA.ID] = tabA
	app.tabOrder = []string{tabA.ID}
	app.activeTabID = tabA.ID

	stubB := &activationStubController{sessionPath: liveB}
	live := &WorkspaceTab{
		ID:            "tab-b",
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       "topic-b",
		TopicTitle:    "Topic B",
		SessionPath:   liveB,
		Ctrl:          stubB,
		Label:         "stub-b",
		Ready:         true,
		disabledMCP:   map[string]ServerView{},
	}
	live.sink = &tabEventSink{tabID: live.ID, app: app}
	installNoopRuntimeEvents(app, live.sink)
	if err := live.ensureSessionLease(liveB); err != nil {
		t.Fatalf("ensureSessionLease B: %v", err)
	}
	app.detachedSessions[sessionRuntimeKey(liveB)] = live

	app.runtimeRebuildMu.Lock()
	defer app.runtimeRebuildMu.Unlock()

	started := time.Now()
	ticket, err := app.StartTopicActivation(TopicActivationRequest{
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       "topic-b",
		SessionPath:   oldB,
		RequestID:     "req-rebuild",
	})
	if err != nil {
		t.Fatalf("StartTopicActivation: %v", err)
	}
	if time.Since(started) > 300*time.Millisecond {
		t.Fatalf("StartTopicActivation blocked %s while MCP rebuild held the mutex", time.Since(started))
	}
	deadline := time.After(400 * time.Millisecond)
	for {
		select {
		case ev := <-events.ch:
			if ev.RequestID == "req-rebuild" && ev.Phase == "ready" {
				if sessionRuntimeKey(ticket.Meta.SessionPath) != sessionRuntimeKey(liveB) {
					t.Fatalf("ticket path = %q, want live %q", ticket.Meta.SessionPath, liveB)
				}
				return
			}
		case <-deadline:
			t.Fatal("ready waited for keepOnlyVisibleTab to acquire the rebuild mutex")
		}
	}
}

func TestPreferLiveSessionPathKeepsExplicitNonRepresentativeInspect(t *testing.T) {
	live := "/sessions/live.jsonl"
	rep := "/sessions/rep.jsonl"
	inspect := "/sessions/inspect.jsonl"
	if got := preferLiveSessionPath(inspect, live, rep); sessionRuntimeKey(got) != sessionRuntimeKey(inspect) {
		t.Fatalf("inspect path = %q, want %q", got, inspect)
	}
	if got := preferLiveSessionPath(rep, live, rep); sessionRuntimeKey(got) != sessionRuntimeKey(live) {
		t.Fatalf("representative path = %q, want live %q", got, live)
	}
	if got := preferLiveSessionPath("", live, rep); sessionRuntimeKey(got) != sessionRuntimeKey(live) {
		t.Fatalf("empty path = %q, want live %q", got, live)
	}
	if got := preferLiveSessionPath(rep, "", rep); sessionRuntimeKey(got) != sessionRuntimeKey(rep) {
		t.Fatalf("no live runtime = %q, want representative", got)
	}
}

func TestPreferLiveSessionPathTreatsRepresentativeAndCanonicalAsOrdinary(t *testing.T) {
	live := "/sessions/live.jsonl"
	parent := "/sessions/parent.jsonl"
	leaf := "/sessions/leaf.jsonl"
	inspect := "/sessions/inspect.jsonl"
	if got := preferLiveSessionPath(parent, live, parent, leaf); sessionRuntimeKey(got) != sessionRuntimeKey(live) {
		t.Fatalf("covering parent = %q, want live %q", got, live)
	}
	if got := preferLiveSessionPath(leaf, live, parent, leaf); sessionRuntimeKey(got) != sessionRuntimeKey(live) {
		t.Fatalf("canonical leaf = %q, want live %q", got, live)
	}
	if got := preferLiveSessionPath(inspect, live, parent, leaf); sessionRuntimeKey(got) != sessionRuntimeKey(inspect) {
		t.Fatalf("history inspect = %q, want %q", got, inspect)
	}
}

func installCoveringLeafTopicCatalog(t *testing.T, app *App) (parent, leaf, live string) {
	t.Helper()
	dir := desktopSessionDir(globalWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	q := provider.Message{Role: provider.RoleUser, Content: "question"}
	a := provider.Message{Role: provider.RoleAssistant, Content: "answer"}
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
	parent = filepath.Join(dir, "root.jsonl")
	leaf = filepath.Join(dir, "leaf.jsonl")
	live = filepath.Join(dir, "live.jsonl")
	save(parent, "conversation", q, a)
	save(leaf, "legacy-leaf-topic", q, a,
		provider.Message{Role: provider.RoleUser, Content: "next"},
		provider.Message{Role: provider.RoleAssistant, Content: "done"})
	if err := agent.SaveBranchMetaPreserveUpdated(leaf, agent.BranchMeta{
		ID: "leaf", Scope: "global", TopicID: "legacy-leaf-topic",
		Recovered: true, ParentID: "root", RecoveryDepth: 1,
	}); err != nil {
		t.Fatal(err)
	}
	save(live, "conversation",
		provider.Message{Role: provider.RoleUser, Content: "today live turn"},
		provider.Message{Role: provider.RoleAssistant, Content: "paused here"})
	installSessionCatalogForTest(t, app, dir, "global", "")
	return parent, leaf, live
}

func TestResolveOpenTopicSessionPathKeepsPausedLiveOnCoveringParent(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	parent, leaf, _ := installCoveringLeafTopicCatalog(t, app)
	paused := &WorkspaceTab{
		ID:             "paused",
		Scope:          "global",
		TopicID:        "conversation",
		SessionPath:    parent,
		Ctrl:           &retargetRuntimeController{status: control.RuntimeStatus{}, path: parent},
		ActivityStatus: topicStatusPaused,
	}
	app.detachedSessions[sessionRuntimeKey(parent)] = paused

	_, resolved := app.resolveOpenTopicSessionPath("global", "", parent)
	if resolved != parent {
		t.Fatalf("resolve paused parent = %q, want keep live parent %q (not covering leaf %q)", resolved, parent, leaf)
	}
}

func TestStartTopicActivationAttachesPausedLiveWhenOpeningCoveringParent(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	events := newActivationEventRecorder(app)
	t.Cleanup(func() { app.shutdown(context.Background()) })

	parent, leaf, livePath := installCoveringLeafTopicCatalog(t, app)
	topic, ok := app.catalogTopicRecord("global", "", "conversation")
	if !ok {
		t.Fatal("catalog topic missing")
	}
	canonical := sessioncatalog.CanonicalSessionPathForTopic(topic.Sessions, "")
	if sessionRuntimeKey(canonical) != sessionRuntimeKey(leaf) {
		t.Fatalf("canonical = %q, want covering leaf %q", canonical, leaf)
	}
	if topic.RepresentativePath != "" && sessionRuntimeKey(topic.RepresentativePath) != sessionRuntimeKey(parent) && sessionRuntimeKey(topic.RepresentativePath) != sessionRuntimeKey(leaf) {
		t.Fatalf("representative = %q, want parent %q or covering leaf %q", topic.RepresentativePath, parent, leaf)
	}

	stub := &activationStubController{sessionPath: livePath, status: &control.RuntimeStatus{}}
	live := &WorkspaceTab{
		ID:             "tab-live",
		Scope:          "global",
		TopicID:        "conversation",
		TopicTitle:     "Upgraded",
		SessionPath:    livePath,
		Ctrl:           stub,
		Label:          "stub-model",
		Ready:          true,
		ActivityStatus: topicStatusPaused,
		disabledMCP:    map[string]ServerView{},
	}
	live.sink = &tabEventSink{tabID: live.ID, app: app}
	installNoopRuntimeEvents(app, live.sink)
	if err := live.ensureSessionLease(livePath); err != nil {
		t.Fatalf("ensureSessionLease live: %v", err)
	}
	app.detachedSessions[sessionRuntimeKey(livePath)] = live

	ticket, err := app.StartTopicActivation(TopicActivationRequest{
		Scope:       "global",
		TopicID:     "conversation",
		SessionPath: parent,
		RequestID:   "req-covering-parent",
	})
	if err != nil {
		t.Fatalf("StartTopicActivation: %v", err)
	}
	events.waitFor(t, activationEventFor("req-covering-parent", "ready"))

	app.mu.RLock()
	tab := app.tabs[ticket.TabID]
	app.mu.RUnlock()
	if tab == nil {
		t.Fatal("activated tab missing")
	}
	if tab.Ctrl != stub {
		t.Fatal("clicking the covering parent opened the catalog leaf instead of attaching the paused live controller")
	}
	if sessionRuntimeKey(tab.currentSessionPath()) != sessionRuntimeKey(livePath) {
		t.Fatalf("session path = %q, want live %q (parent %q leaf %q)", tab.currentSessionPath(), livePath, parent, leaf)
	}
	if ticket.Meta.SessionPath != "" && sessionRuntimeKey(ticket.Meta.SessionPath) != sessionRuntimeKey(livePath) {
		t.Fatalf("ticket session path = %q, want live %q", ticket.Meta.SessionPath, livePath)
	}
}
