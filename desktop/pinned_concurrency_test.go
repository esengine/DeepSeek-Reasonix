package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/store"
	"reasonix/internal/tool"
)

type blockingPinnedProvider struct {
	started    chan struct{}
	release    chan struct{}
	cancelled  chan struct{}
	once       sync.Once
	cancelOnce sync.Once
}

func (p *blockingPinnedProvider) Name() string { return "blocking-pinned" }

func (p *blockingPinnedProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.once.Do(func() { close(p.started) })
	if p.cancelled == nil {
		<-p.release
	} else {
		select {
		case <-p.release:
		case <-ctx.Done():
			p.cancelOnce.Do(func() { close(p.cancelled) })
			<-p.release
		}
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func pinnedConcurrencyFixture(t *testing.T, prov provider.Provider) (*App, *WorkspaceTab, *control.Controller, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "session.jsonl")
	exec := agent.New(prov, tool.NewRegistry(), agent.NewSession("BASE"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{
		Runner:              exec,
		Executor:            exec,
		SystemPrompt:        "BASE",
		PinnedContextLoader: pinnedContextLoader(root),
		SessionDir:          root,
		SessionPath:         path,
		Label:               "test",
		Sink:                event.Discard,
	})
	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID:            "pinned-race",
		Scope:         "project",
		WorkspaceRoot: root,
		SessionPath:   path,
		Ready:         true,
		Ctrl:          ctrl,
		disabledMCP:   map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(ctrl.Close)
	return app, tab, ctrl, path
}

func TestPinAndUnpinRejectWhileTurnIsRunning(t *testing.T) {
	prov := &blockingPinnedProvider{started: make(chan struct{}), release: make(chan struct{})}
	app, tab, ctrl, path := pinnedConcurrencyFixture(t, prov)
	if err := os.WriteFile(filepath.Join(tab.WorkspaceRoot, "context.md"), []byte("context"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.PinFileForTab(tab.ID, "context.md"); err != nil {
		t.Fatalf("initial pin: %v", err)
	}
	turnDone := make(chan error, 1)
	go func() { turnDone <- ctrl.RunTurn(context.Background(), "hold") }()
	select {
	case <-prov.started:
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not reach provider")
	}

	if err := app.UnpinFileForTab(tab.ID, "context.md"); err == nil {
		t.Fatal("Unpin succeeded while a turn was running")
	}
	if _, err := app.PinFileForTab(tab.ID, "context.md"); err == nil {
		t.Fatal("duplicate Pin succeeded while a turn was running")
	}
	state, err := loadPinnedContextState(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 1 || state.Files[0] != "context.md" {
		t.Fatalf("busy mutations changed sidecar: %v", state.Files)
	}
	close(prov.release)
	if err := <-turnDone; err != nil {
		t.Fatalf("turn: %v", err)
	}
}

func TestGetPinnedFilesForTabReturnsNonNilEmptyList(t *testing.T) {
	app, tab, _, _ := pinnedConcurrencyFixture(t, nil)
	infos, err := app.GetPinnedFilesForTab(tab.ID)
	if err != nil {
		t.Fatal(err)
	}
	if infos == nil || len(infos) != 0 {
		t.Fatalf("empty pinned files = %#v, want []", infos)
	}
}

func TestGetPinnedFilesForTabDoesNotOverwriteNewerCachedPins(t *testing.T) {
	app, tab, _, path := pinnedConcurrencyFixture(t, nil)
	for _, name := range []string{"old.md", "new.md"} {
		if err := os.WriteFile(filepath.Join(tab.WorkspaceRoot, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := savePinnedContextState(path, []string{"old.md"}); err != nil {
		t.Fatal(err)
	}

	// Model the stale-read window: Get loaded the old sidecar while a newer
	// Pin/Unpin or session binding already published the current tab cache.
	tab.setPinnedFiles([]string{"new.md"})
	infos, err := app.GetPinnedFilesForTab(tab.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Path != "old.md" {
		t.Fatalf("GetPinnedFilesForTab = %#v, want old sidecar snapshot", infos)
	}
	if got := tab.GetPinnedFiles(); len(got) != 1 || got[0] != "new.md" {
		t.Fatalf("read-only GetPinnedFilesForTab overwrote newer cache: %v", got)
	}
}

func TestNewSessionWaitsForPinAndClearsItsResult(t *testing.T) {
	isolateDesktopUserDirs(t)
	oldRef, _ := configureSwitchableDefaultModels(t)
	root := globalWorkspaceRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path, err := createEmptySessionFile(desktopSessionDir(root), "old-model")
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.SetBranchModelPreserveUpdated(path, oldRef); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "context.md"), []byte("context"), 0o600); err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, agent.NewSession("BASE"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SystemPrompt: "BASE", SessionDir: desktopSessionDir(root), SessionPath: path, Label: oldRef, Sink: event.Discard})
	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID: "pin-new-race", Scope: "global", WorkspaceRoot: root, SessionPath: path,
		Ready: true, Ctrl: ctrl, model: oldRef, disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})

	readStarted := make(chan struct{})
	releaseRead := make(chan struct{})
	var once sync.Once
	hook := func() {
		once.Do(func() { close(readStarted) })
		<-releaseRead
	}
	pinnedFileReadHookForTest.Store(&hook)
	t.Cleanup(func() { pinnedFileReadHookForTest.Store(nil) })
	pinDone := make(chan error, 1)
	go func() {
		_, err := app.PinFileForTab(tab.ID, "context.md")
		pinDone <- err
	}()
	select {
	case <-readStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("Pin did not reach file read")
	}
	newBeforeLock := make(chan struct{})
	var newBeforeLockOnce sync.Once
	app.runtimeMutationBeforeLockHook = func(operation string) {
		if operation == "new session" {
			newBeforeLockOnce.Do(func() { close(newBeforeLock) })
		}
	}
	newDone := make(chan error, 1)
	go func() { newDone <- app.NewSessionForTab(tab.ID) }()
	select {
	case <-newBeforeLock:
	case <-time.After(5 * time.Second):
		t.Fatal("NewSession did not reach the runtime mutation barrier")
	}
	select {
	case err := <-newDone:
		t.Fatalf("NewSession bypassed in-flight Pin: %v", err)
	default:
	}
	close(releaseRead)
	if err := <-pinDone; err != nil {
		t.Fatalf("PinFileForTab: %v", err)
	}
	if err := <-newDone; err != nil {
		t.Fatalf("NewSessionForTab: %v", err)
	}
	state, err := loadPinnedContextState(tab.currentSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Files) != 0 || len(tab.GetPinnedFiles()) != 0 {
		t.Fatalf("new session inherited racing pin: sidecar=%v cache=%v", state.Files, tab.GetPinnedFiles())
	}
	if _, err := os.Stat(store.SessionPinnedContext(tab.currentSessionPath())); err != nil {
		t.Fatalf("new session did not write an empty pinned sidecar: %v", err)
	}
	if got := tab.Ctrl.SystemPrompt(); got == "" || strings.Contains(got, "<pinned_context>") {
		t.Fatalf("new controller retained pinned prompt: %q", got)
	}
}

func TestRunningClearDoesNotMigrateOldPinnedCache(t *testing.T) {
	isolateDesktopUserDirs(t)
	oldRef, _ := configureSwitchableDefaultModels(t)
	prov := &blockingPinnedProvider{started: make(chan struct{}), release: make(chan struct{}), cancelled: make(chan struct{})}
	app, tab, ctrl, oldPath := pinnedConcurrencyFixture(t, prov)
	t.Cleanup(func() {
		if tab.Ctrl != nil && tab.Ctrl != ctrl {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})
	tab.model = oldRef
	if err := os.WriteFile(filepath.Join(tab.WorkspaceRoot, "context.md"), []byte("context"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.PinFileForTab(tab.ID, "context.md"); err != nil {
		t.Fatal(err)
	}
	go ctrl.Submit("hold")
	select {
	case <-prov.started:
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not start")
	}
	clearDone := make(chan error, 1)
	go func() {
		_, err := app.ClearSessionForTab(tab.ID)
		clearDone <- err
	}()
	select {
	case <-prov.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("clear did not cancel the running turn")
	}
	select {
	case err := <-clearDone:
		t.Fatalf("clear completed before the blocked provider stopped: %v", err)
	default:
	}
	close(prov.release)
	if err := <-clearDone; err != nil {
		t.Fatalf("ClearSessionForTab: %v", err)
	}
	if _, err := os.Stat(store.SessionPinnedContext(oldPath)); !os.IsNotExist(err) {
		t.Fatalf("old pinned sidecar survived clear: %v", err)
	}
	if got := tab.GetPinnedFiles(); len(got) != 0 {
		t.Fatalf("running clear retained cached pins: %v", got)
	}
	state, err := loadPinnedContextState(tab.currentSessionPath())
	if err != nil || len(state.Files) != 0 {
		t.Fatalf("replacement pins = %+v, err=%v", state, err)
	}
	if _, err := os.Stat(store.SessionPinnedContext(tab.currentSessionPath())); err != nil {
		t.Fatalf("running clear did not write an empty pinned sidecar: %v", err)
	}
}
