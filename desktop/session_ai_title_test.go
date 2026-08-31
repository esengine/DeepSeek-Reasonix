package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncatalog"
)

type desktopSessionTitleProvider struct {
	started chan struct{}
	chunks  chan provider.Chunk
}

func (p *desktopSessionTitleProvider) Name() string { return "desktop-session-title" }

func (p *desktopSessionTitleProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	if p.started != nil {
		close(p.started)
	}
	return p.chunks, nil
}

func newDesktopSessionTitleController(dir, path string, prov provider.Provider) *control.Controller {
	return control.New(control.Options{
		SessionDir:  dir,
		SessionPath: path,
		ModelRef:    "test/title-model",
		ProviderResolver: &provider.StaticResolver{
			Descriptors: []provider.Descriptor{{Ref: "test/title-model"}},
			Providers:   map[string]provider.Provider{"test/title-model": prov},
		},
	})
}

func installDesktopSessionTitleTab(app *App, ctrl *control.Controller, topicID, path string) {
	app.setTestCtrl(ctrl, "test/title-model")
	app.mu.Lock()
	tab := app.tabs["test"]
	tab.TopicID = topicID
	tab.SessionPath = path
	app.mu.Unlock()
}

func TestAIRenameSessionWritesCanonicalAndLegacyTitles(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	path := agent.NewSessionPath(dir, "title-test")
	writeHistoryTestSession(t, path, "debug the login redirect loop")
	chunks := make(chan provider.Chunk, 2)
	chunks <- provider.Chunk{Type: provider.ChunkText, Text: `"Debug login redirect loop"`}
	chunks <- provider.Chunk{Type: provider.ChunkDone}
	close(chunks)
	ctrl := newDesktopSessionTitleController(dir, path, &desktopSessionTitleProvider{chunks: chunks})
	app := NewApp()
	installDesktopSessionTitleTab(app, ctrl, "topic-login", path)
	defer ctrl.Close()

	title, err := app.AIRenameSession("topic-login")
	if err != nil {
		t.Fatalf("AIRenameSession: %v", err)
	}
	if title != "Debug login redirect loop" {
		t.Fatalf("title = %q", title)
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok || meta.CustomTitle != title {
		t.Fatalf("meta = %+v, ok=%v, err=%v", meta, ok, err)
	}
	if got := loadSessionTitles(dir)[filepath.Base(path)]; got != title {
		t.Fatalf("legacy title = %q", got)
	}
}

func TestAIRenameSessionRejectsStaleProviderCompletion(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	first := agent.NewSessionPath(dir, "first")
	second := agent.NewSessionPath(dir, "second")
	writeHistoryTestSession(t, first, "first conversation")
	writeHistoryTestSession(t, second, "second conversation")
	started := make(chan struct{})
	chunks := make(chan provider.Chunk, 2)
	ctrl := newDesktopSessionTitleController(dir, first, &desktopSessionTitleProvider{started: started, chunks: chunks})
	app := NewApp()
	installDesktopSessionTitleTab(app, ctrl, "topic-race", first)
	defer ctrl.Close()

	result := make(chan error, 1)
	go func() {
		_, err := app.AIRenameSession("topic-race")
		result <- err
	}()
	<-started
	ctrl.SetSessionPath(second)
	app.mu.Lock()
	app.tabs["test"].SessionPath = second
	app.mu.Unlock()
	chunks <- provider.Chunk{Type: provider.ChunkText, Text: "Stale title"}
	chunks <- provider.Chunk{Type: provider.ChunkDone}
	close(chunks)

	if err := <-result; err == nil || !strings.Contains(err.Error(), "session changed") {
		t.Fatalf("stale completion error = %v", err)
	}
	for _, path := range []string{first, second} {
		meta, ok, err := agent.LoadBranchMeta(path)
		if err != nil {
			t.Fatal(err)
		}
		if ok && meta.CustomTitle != "" {
			t.Fatalf("stale completion renamed %s to %q", path, meta.CustomTitle)
		}
	}
}

func TestAIRenameSessionRejectsCompletionAfterManualRename(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	path := agent.NewSessionPath(dir, "manual-wins")
	writeHistoryTestSession(t, path, "original conversation")
	started := make(chan struct{})
	chunks := make(chan provider.Chunk, 2)
	ctrl := newDesktopSessionTitleController(dir, path, &desktopSessionTitleProvider{started: started, chunks: chunks})
	app := NewApp()
	installDesktopSessionTitleTab(app, ctrl, "topic-manual", path)
	defer ctrl.Close()

	result := make(chan error, 1)
	go func() {
		_, err := app.AIRenameSession("topic-manual")
		result <- err
	}()
	<-started
	if err := app.RenameSession(path, "Newer manual title"); err != nil {
		t.Fatal(err)
	}
	chunks <- provider.Chunk{Type: provider.ChunkText, Text: "Stale AI title"}
	chunks <- provider.Chunk{Type: provider.ChunkDone}
	close(chunks)

	if err := <-result; err == nil || !strings.Contains(err.Error(), "title changed") {
		t.Fatalf("stale AI completion error = %v", err)
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok || meta.CustomTitle != "Newer manual title" {
		t.Fatalf("manual title was overwritten: meta=%+v ok=%v err=%v", meta, ok, err)
	}
}

func TestDelayedTitleCallbackProjectsCurrentCanonicalTitle(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	path := agent.NewSessionPath(dir, "projection-race")
	writeHistoryTestSession(t, path, "original conversation")
	app := NewApp()

	if err := agent.RenameSession(path, "AI title"); err != nil {
		t.Fatal(err)
	}
	if err := agent.RenameSession(path, "Newer manual title"); err != nil {
		t.Fatal(err)
	}
	if err := app.onSessionTitleChanged(dir, path, "Newer manual title"); err != nil {
		t.Fatal(err)
	}
	// Simulate the older AI callback resuming after the newer manual callback.
	if err := app.onSessionTitleChanged(dir, path, "AI title"); err != nil {
		t.Fatal(err)
	}

	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok || meta.CustomTitle != "Newer manual title" {
		t.Fatalf("canonical title = %+v, ok=%v, err=%v", meta, ok, err)
	}
	if got := loadSessionTitles(dir)[filepath.Base(path)]; got != "Newer manual title" {
		t.Fatalf("legacy projection = %q, want canonical newer title", got)
	}
}

func TestSessionVersionNoteDoesNotReplaceTopicTitle(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	topicID := "metadata-title"
	path := writeTopicSessionWithPrompt(t, dir, "metadata-title.jsonl", topicID, "Original topic", "", "first prompt", time.Now())
	if err := ensureTopicIndexed("global", "", topicID, "Original topic", topicTitleSourceManual); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	installSessionCatalogForTest(t, app, dir, "global", "")
	catalog := app.sessionCatalog.Load()
	if err := app.syncSessionCatalogMetadata(context.Background(), catalog); err != nil {
		t.Fatal(err)
	}
	if err := agent.RenameSession(path, "AI session title"); err != nil {
		t.Fatal(err)
	}
	if err := catalog.IndexSessionPath(context.Background(), sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}, path); err != nil {
		t.Fatal(err)
	}
	if err := app.syncSessionCatalogMetadata(context.Background(), catalog); err != nil {
		t.Fatal(err)
	}
	page, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "global", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Label != "Original topic" {
		t.Fatalf("version note replaced topic title: %+v", page.Items)
	}
	if meta, ok, err := agent.LoadBranchMeta(path); err != nil || !ok || meta.CustomTitle != "AI session title" {
		t.Fatalf("version note was not preserved: meta=%+v ok=%v err=%v", meta, ok, err)
	}
}

func TestSessionTitleTranscriptAndPreviewAreBounded(t *testing.T) {
	transcript := sessionTitleTranscript([]string{
		strings.Repeat("a", aiSessionTitleMaxTurnRunes+10), "second", "third", "ignored",
	})
	parts := strings.Split(transcript, "\n\n")
	if len(parts) != aiSessionTitleMaxTurns || len([]rune(parts[0])) != aiSessionTitleMaxTurnRunes {
		t.Fatalf("parts = %d first runes = %d", len(parts), len([]rune(parts[0])))
	}
	records := []sessioncatalog.SessionRecord{{Path: "/sessions/a.jsonl", Preview: "full first-message preview"}}
	if got := topicSessionPreview(records, "/sessions/a.jsonl"); got != "full first-message preview" {
		t.Fatalf("preview = %q", got)
	}
}

func TestControllerForTopicPrefersActiveAndRejectsAmbiguousBackgroundTabs(t *testing.T) {
	app := NewApp()
	ctrlA := control.New(control.Options{})
	ctrlB := control.New(control.Options{})
	defer ctrlA.Close()
	defer ctrlB.Close()
	app.tabs = map[string]*WorkspaceTab{
		"a": {ID: "a", TopicID: "shared", Ctrl: ctrlA, Ready: true},
		"b": {ID: "b", TopicID: "shared", Ctrl: ctrlB, Ready: true},
	}
	app.activeTabID = "b"
	if got := app.controllerForTopic("shared"); got != ctrlB {
		t.Fatalf("active controller = %p, want %p", got, ctrlB)
	}
	app.activeTabID = "other"
	if got := app.controllerForTopic("shared"); got != nil {
		t.Fatalf("ambiguous background controller = %p, want nil", got)
	}
}
