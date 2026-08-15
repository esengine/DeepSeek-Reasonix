package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/control"
)

func TestProjectTreeRuntimeSnapshotWailsArraysAreNonNil(t *testing.T) {
	snapshot := NewApp().GetProjectTreeRuntimeSnapshot()
	raw, err := json.Marshal(snapshot)
	if err != nil || snapshot.Topics == nil || !strings.Contains(string(raw), `"topics":[]`) {
		t.Fatalf("empty runtime snapshot = %s (%v), want non-nil topics:[]", raw, err)
	}
}

func TestProjectTreeRuntimeSnapshotLocalizesAutoTopicTitle(t *testing.T) {
	app := NewApp()
	app.setDesktopLocale("en-US")
	app.tabs["auto"] = &WorkspaceTab{
		ID: "auto", Scope: "global", TopicID: "topic-auto",
		TopicTitle: defaultTopicTitle, topicTitleSource: topicTitleSourceAuto,
	}

	snapshot := app.GetProjectTreeRuntimeSnapshot()
	if len(snapshot.Topics) != 1 || snapshot.Topics[0].Node.Label != defaultTopicTitleEn {
		t.Fatalf("runtime topic = %+v, want localized %q", snapshot.Topics, defaultTopicTitleEn)
	}
}

func TestProjectTreeRuntimeSnapshotFindsRestoredTabBeforeFirstEvent(t *testing.T) {
	app := NewApp()
	app.tabs["restored"] = &WorkspaceTab{
		ID: "restored", Scope: "project", WorkspaceRoot: "/workspace/restored",
		TopicID: "topic-restored", TopicTitle: "Restored task",
		SessionPath: "/sessions/restored.jsonl",
		Ctrl:        &activationStubController{sessionPath: "/sessions/restored.jsonl"},
		Ready:       true,
	}
	snapshot := app.GetProjectTreeRuntimeSnapshot()
	if snapshot.Revision != 0 || len(snapshot.Topics) != 1 {
		t.Fatalf("initial runtime snapshot = %+v, want one topic at revision 0", snapshot)
	}
	topic := snapshot.Topics[0]
	if topic.Node.TopicID != "topic-restored" || !topic.Node.Open || !topic.Node.Running {
		t.Fatalf("restored runtime topic = %+v, want open/running topic-restored", topic)
	}
}

func TestSyncProjectRootSpellingIncludesDetachedRuntime(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := normalizeProjectRoot(t.TempDir())
	if err := addProject(root, "Project"); err != nil {
		t.Fatalf("add project: %v", err)
	}
	app := NewApp()
	app.detachedSessions["detached"] = &WorkspaceTab{
		Scope: "project", WorkspaceRoot: root + string(os.PathSeparator) + ".",
	}
	app.syncTabWorkspaceRootSpellings()
	if got := app.detachedSessions["detached"].WorkspaceRoot; got != root {
		t.Fatalf("detached runtime root = %q, want canonical %q", got, root)
	}
}

func TestKeepOnlyVisibleTabPublishesDetachedRuntimeProjection(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectA := t.TempDir()
	projectB := t.TempDir()
	for _, dir := range []string{desktopSessionDir(projectA), desktopSessionDir(projectB)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir session dir: %v", err)
		}
	}
	sessionA := writeTopicSessionWithPrompt(
		t,
		desktopSessionDir(projectA),
		"running-a.jsonl",
		"topic-a",
		"Running A",
		projectA,
		"keep working in project A",
		time.Now(),
	)
	sessionB := writeTopicSessionWithPrompt(
		t,
		desktopSessionDir(projectB),
		"visible-b.jsonl",
		"topic-b",
		"Visible B",
		projectB,
		"switch to project B",
		time.Now(),
	)

	app := NewApp()
	app.ctx = context.Background()
	events := make(chan runtimeEventEnvelope, 8)
	app.runtimeEvents.emit = func(ctx context.Context, name string, payload ...any) {
		events <- runtimeEventEnvelope{ctx: ctx, name: name, payload: append([]any(nil), payload...)}
	}
	t.Cleanup(func() { app.shutdown(context.Background()) })

	runningCtrl := &activationStubController{sessionPath: sessionA}
	visibleCtrl := &activationStubController{sessionPath: sessionB, status: &control.RuntimeStatus{}}
	running := &WorkspaceTab{
		ID: "tab-a", Scope: "project", WorkspaceRoot: projectA,
		TopicID: "topic-a", TopicTitle: "Running A", SessionPath: sessionA,
		Ctrl: runningCtrl, Ready: true, disabledMCP: map[string]ServerView{},
	}
	visible := &WorkspaceTab{
		ID: "tab-b", Scope: "project", WorkspaceRoot: projectB,
		TopicID: "topic-b", TopicTitle: "Visible B", SessionPath: sessionB,
		Ctrl: visibleCtrl, Ready: true, disabledMCP: map[string]ServerView{},
	}
	running.sink = &tabEventSink{tabID: running.ID, app: app}
	visible.sink = &tabEventSink{tabID: visible.ID, app: app}
	app.tabs[running.ID] = running
	app.tabs[visible.ID] = visible
	app.tabOrder = []string{running.ID, visible.ID}
	app.activeTabID = running.ID

	if _, err := app.keepOnlyVisibleTab(visible.ID); err != nil {
		t.Fatalf("keepOnlyVisibleTab: %v", err)
	}
	app.mu.RLock()
	detached := app.detachedSessions[sessionRuntimeKey(sessionA)]
	app.mu.RUnlock()
	if detached != running || runningCtrl.closed.Load() {
		t.Fatal("project A's running session was not preserved as a detached runtime")
	}

	deadline := time.After(time.Second)
	for {
		select {
		case emitted := <-events:
			if emitted.name != "project-tree:runtime-changed" {
				continue
			}
			if len(emitted.payload) != 1 {
				t.Fatalf("project-tree:runtime-changed payload count = %d, want 1", len(emitted.payload))
			}
			event, ok := emitted.payload[0].(ProjectTreeRuntimeSnapshot)
			if !ok {
				t.Fatalf("project-tree:runtime-changed payload type = %T, want ProjectTreeRuntimeSnapshot", emitted.payload[0])
			}
			if event.Topics == nil || event.Revision == 0 {
				t.Fatalf("project-tree:runtime-changed event = %+v, want a versioned runtime snapshot", event)
			}
			found := false
			for _, topic := range event.Topics {
				if topic.Scope == "project" && sameProjectRoot(topic.WorkspaceRoot, projectA) && topic.Node.TopicID == "topic-a" {
					found = !topic.Node.Open && topic.Node.Running
				}
			}
			if !found {
				t.Fatalf("detached runtime topic missing from snapshot: %+v", event.Topics)
			}
			return
		case <-deadline:
			t.Fatal("detaching project A emitted no runtime snapshot; the running conversation stays invisible")
		}
	}
}
