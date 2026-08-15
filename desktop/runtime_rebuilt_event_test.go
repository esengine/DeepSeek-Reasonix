package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TestRuntimeRebuildsEmitRuntimeRebuiltForTab pins the chime-dedupe contract:
// model/effort rebuilds emit runtime:rebuilt; deprecated SetTokenMode does not.
func TestRuntimeRebuildsEmitRuntimeRebuiltForTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")
	setDesktopTestCredential(t, "NEW_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old", "new"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "old", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "old-model", APIKeyEnv: "OLD_MODEL_KEY"},
		{Name: "new", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "deepseek-v4-pro", APIKeyEnv: "NEW_MODEL_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	path := filepath.Join(dir, "rebuild-events.jsonl")
	ctrl := control.New(control.Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "old", Sink: event.Discard})

	app := NewApp()
	app.ctx = context.Background()
	// emitReady calls the Wails runtime directly; the ready hook keeps the
	// workspace-reconcile path (which SetEffortForTab can take) off the real
	// event bridge, which log.Fatals on a plain Background context.
	app.readyHook = func() {}

	var mu sync.Mutex
	var rebuilt []string
	// The App-level queue must stay silent: ordering against the tab's agent
	// events only holds when the notice rides the tab sink's own queue, so a
	// notice showing up here means the routing regressed to the fallback.
	app.runtimeEvents.emit = func(_ context.Context, name string, _ ...any) {
		if name == "runtime:rebuilt" {
			mu.Lock()
			rebuilt = append(rebuilt, "VIA-APP-QUEUE")
			mu.Unlock()
		}
	}
	sinkEmit := func(_ context.Context, name string, payload ...any) {
		if name != "runtime:rebuilt" {
			return
		}
		tabID := ""
		if len(payload) > 0 {
			tabID, _ = payload[0].(string)
		}
		mu.Lock()
		rebuilt = append(rebuilt, tabID)
		mu.Unlock()
	}

	tab := &WorkspaceTab{
		ID:            "tab_rebuild_events",
		Scope:         "global",
		WorkspaceRoot: globalTabWorkspaceRoot(),
		Ready:         true,
		model:         "old/old-model",
		Ctrl:          ctrl,
		sink:          &tabEventSink{tabID: "tab_rebuild_events", app: app, ctx: context.Background()},
		disabledMCP:   map[string]ServerView{},
	}
	tab.sink.runtimeEvents.emit = sinkEmit
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
	})

	waitCount := func(want int, step string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			n := len(rebuilt)
			mu.Unlock()
			if n >= want {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("after %s: runtime:rebuilt events = %v, want %d", step, rebuilt, want)
	}

	if err := app.SetModelForTab(tab.ID, "new/deepseek-v4-pro"); err != nil {
		t.Fatalf("SetModelForTab: %v", err)
	}
	waitCount(1, "model switch")

	if err := app.SetEffortForTab(tab.ID, "high"); err != nil {
		t.Fatalf("SetEffortForTab: %v", err)
	}
	waitCount(2, "effort switch")

	if err := app.SetTokenModeForTab(tab.ID, "economy"); err != nil {
		t.Fatalf("SetTokenModeForTab: %v", err)
	}
	// Give a real rebuild event time to arrive if the no-op regresses.
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(rebuilt) != 2 {
		t.Fatalf("after SetTokenModeForTab: runtime:rebuilt events = %v, want 2 (no rebuild)", rebuilt)
	}
	for i, id := range rebuilt {
		if id == "VIA-APP-QUEUE" {
			t.Fatalf("event %d took the App-level fallback queue; it must ride the tab sink queue so it orders before the rebuilt controller's agent events (full: %v)", i, rebuilt)
		}
		if id != tab.ID {
			t.Fatalf("event %d carried tab id %q, want %q (full: %v)", i, id, tab.ID, rebuilt)
		}
	}
	mu.Unlock()
}

// TestRuntimeReattachFencesPendingAskBeforeReplay pins the detached-runtime
// handoff order. A transferred controller keeps its pending ask, but the
// frontend must learn the transferred epoch before that ask reaches it.
func TestRuntimeReattachFencesPendingAskBeforeReplay(t *testing.T) {
	type emittedEvent struct {
		name    string
		payload []any
	}
	emitted := make(chan emittedEvent, 4)
	sink := &tabEventSink{
		tabID:        "tab-reattach",
		ctx:          context.Background(),
		runtimeEpoch: "runtime-new",
	}
	sink.runtimeEvents.emit = func(_ context.Context, name string, payload ...any) {
		emitted <- emittedEvent{name: name, payload: payload}
	}

	ctrl := control.New(control.Options{Sink: sink})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = ctrl.Ask(ctx, []event.AskQuestion{{ID: "choice", Prompt: "Pick one"}})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		ctrl.Close()
	})

	select {
	case initial := <-emitted:
		if initial.name != eventChannel {
			t.Fatalf("initial event = %q, want %q", initial.name, eventChannel)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial ask")
	}

	app := NewApp()
	tab := &WorkspaceTab{ID: "tab-reattach", Ctrl: ctrl, sink: sink, Ready: true}
	app.replayPendingPromptsAfterRuntimeAttach(tab.ID, sink, ctrl, "runtime-new")

	var got []emittedEvent
	for len(got) < 2 {
		select {
		case next := <-emitted:
			got = append(got, next)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for reattach events; got %+v", got)
		}
	}
	if got[0].name != "runtime:rebuilt" || got[1].name != eventChannel {
		t.Fatalf("reattach event order = [%s, %s], want [runtime:rebuilt, %s]", got[0].name, got[1].name, eventChannel)
	}
	if len(got[0].payload) < 2 || got[0].payload[0] != tab.ID || got[0].payload[1] != "runtime-new" {
		t.Fatalf("runtime:rebuilt payload = %#v", got[0].payload)
	}
}
