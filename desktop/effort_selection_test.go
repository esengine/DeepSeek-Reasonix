package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

const effortFinalReply = `{"choices":[{"delta":{"content":"Done."},"finish_reason":"stop"}]}`
const effortReadReply = `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"read-fixture","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"fixture.txt\"}"}}]},"finish_reason":"tool_calls"}]}`

type selectedEffortRequest struct {
	effort string
	reply  chan string
}

type effortSelectionFixture struct {
	app      *App
	tab      *WorkspaceTab
	ctx      context.Context
	requests chan selectedEffortRequest
	count    atomic.Int32
}

func newEffortSelectionFixture(t *testing.T) *effortSelectionFixture {
	t.Helper()
	isolateDesktopUserDirs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	f := &effortSelectionFixture{ctx: ctx, requests: make(chan selectedEffortRequest, 8)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Effort    string `json:"reasoning_effort"`
			MaxTokens int    `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		// Session title generation is not a foreground model request.
		if body.MaxTokens == 512 {
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", effortFinalReply)
			return
		}
		f.count.Add(1)
		request := selectedEffortRequest{effort: body.Effort, reply: make(chan string, 1)}
		select {
		case f.requests <- request:
		case <-ctx.Done():
			return
		}
		select {
		case response := <-request.reply:
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", response)
		case <-r.Context().Done():
		case <-ctx.Done():
		}
	}))
	t.Cleanup(server.Close)
	cfg := config.Default()
	cfg.DefaultModel = "effort-test/m"
	cfg.Desktop.ProviderAccess = []string{"effort-test"}
	cfg.Providers = []config.ProviderEntry{{
		Name: "effort-test", Kind: "openai", BaseURL: server.URL, Model: "m", Models: []string{"m", "other"},
		APIKeyEnv: "EFFORT_SELECTION_TEST_KEY", NoProxy: true,
		ReasoningProtocol: config.ReasoningProtocolOpenAI, SupportedEfforts: []string{"low", "high", "max"}, Effort: "low",
	}}
	setDesktopTestCredential(t, "EFFORT_SELECTION_TEST_KEY", "test-only")
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	f.app = NewApp()
	f.app.ctx = ctx
	f.app.readyHook = func() {}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fixture.txt"), []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f.tab = &WorkspaceTab{ID: "effort", Scope: "project", WorkspaceRoot: root, Ready: true,
		model: cfg.DefaultModel, disabledMCP: map[string]ServerView{}}
	f.tab.sink = &tabEventSink{tabID: f.tab.ID, app: f.app}
	installNoopRuntimeEvents(f.app, f.tab.sink)
	ctrl, err := boot.Build(ctx, boot.Options{Model: cfg.DefaultModel, WorkspaceRoot: root,
		SessionDir: desktopSessionDir(root), Sink: f.tab.sink, BeforeInboxDispatch: f.app.beforeInboxDispatch,
		OnSessionTransition: f.app.handleTabSessionTransition(f.tab)})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ctrl.SessionDir(), "effort.jsonl")
	ctrl.AdoptHistory(append(ctrl.History(), provider.Message{Role: provider.RoleUser, Content: "Keep history."}), path)
	f.app.mu.Lock()
	f.tab.Ctrl = ctrl
	f.tab.SessionPath = path
	f.app.tabs = map[string]*WorkspaceTab{f.tab.ID: f.tab}
	f.app.tabOrder = []string{f.tab.ID}
	f.app.activeTabID = f.tab.ID
	f.app.mu.Unlock()
	if err := f.tab.ensureSessionLease(path); err != nil {
		t.Fatal(err)
	}
	if err := bindTabWriteAuthority(f.tab, ctrl); err != nil {
		t.Fatal(err)
	}
	if err := f.app.saveTabSessionMeta(f.tab, path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if current := f.app.controllerForTab(f.tab); current != nil {
			waitForAutosaveIdle(t, f.tab)
			current.Close()
		}
		f.tab.releaseSessionLease()
	})
	t.Cleanup(cancel)
	return f
}

func (f *effortSelectionFixture) request(t *testing.T, want string) selectedEffortRequest {
	t.Helper()
	select {
	case request := <-f.requests:
		if request.effort != want {
			t.Fatalf("request effort = %q, want %q", request.effort, want)
		}
		return request
	case <-f.ctx.Done():
		t.Fatal("waiting for provider request:", f.ctx.Err())
		return selectedEffortRequest{}
	}
}

func (f *effortSelectionFixture) selectEffort(t *testing.T, value string) {
	t.Helper()
	if err := f.app.SetEffortForTab(f.tab.ID, value); err != nil {
		t.Fatal(err)
	}
}

func (f *effortSelectionFixture) assertEffort(t *testing.T, current, pending string) {
	t.Helper()
	info := f.app.EffortForTab(f.tab.ID)
	if !info.CanDefer || info.Current != current {
		t.Fatalf("effort = %+v, want current %q and local deferral", info, current)
	}
	if pending == "" {
		if info.Pending != nil {
			t.Fatalf("unexpected pending effort %q", *info.Pending)
		}
	} else if info.Pending == nil || *info.Pending != pending {
		t.Fatalf("effort = %+v, want pending %q", info, pending)
	}
}

func TestEffortSelectionAppliesOnlyAtNextRun(t *testing.T) {
	for _, mode := range []string{"manual", "inbox"} {
		t.Run(mode, func(t *testing.T) {
			f := newEffortSelectionFixture(t)
			old := f.tab.Ctrl
			if err := f.app.SubmitToTab(f.tab.ID, "Read fixture.txt and answer done."); err != nil {
				t.Fatal(err)
			}
			first := f.request(t, "low")
			f.selectEffort(t, "high")
			f.selectEffort(t, "max")
			f.assertEffort(t, "low", "max")
			if f.app.controllerForTab(f.tab) != old {
				t.Fatal("selection replaced a running controller")
			}
			if mode == "inbox" {
				if _, err := f.app.EnqueueInboxFollowup(f.tab.ID, "Say done again.", "Say done again.", "effort-next"); err != nil {
					t.Fatal(err)
				}
			}
			first.reply <- effortReadReply
			// Even the subsequent request in the same task uses the old effort.
			second := f.request(t, "low")
			second.reply <- effortFinalReply
			if mode == "manual" {
				waitNotRunning(t, old)
				f.assertEffort(t, "low", "max")
				if f.count.Load() != 2 {
					t.Fatal("selection started a new task on its own")
				}
				if err := f.app.SubmitToTab(f.tab.ID, "Say done again."); err != nil {
					t.Fatal(err)
				}
			}
			next := f.request(t, "max")
			f.assertEffort(t, "max", "")
			next.reply <- effortFinalReply
			waitNotRunning(t, f.app.controllerForTab(f.tab))
			if f.count.Load() != 3 {
				t.Fatalf("provider called %d times, want exactly three", f.count.Load())
			}
			if f.app.controllerForTab(f.tab) == old {
				t.Fatal("next task did not replace the old runtime")
			}
			var foundFirst bool
			for _, message := range f.tab.Ctrl.History() {
				foundFirst = foundFirst || strings.Contains(message.Content, "Read fixture.txt")
			}
			if !foundFirst {
				t.Fatal("effort change lost earlier history")
			}
		})
	}
}

func TestEffortSelectionCancellationStopAndPersistence(t *testing.T) {
	f := newEffortSelectionFixture(t)
	old := f.tab.Ctrl
	if err := f.app.SubmitToTab(f.tab.ID, "Say done."); err != nil {
		t.Fatal(err)
	}
	f.request(t, "low")
	f.selectEffort(t, "max")
	f.selectEffort(t, "low")
	f.assertEffort(t, "low", "")
	f.selectEffort(t, "auto")
	f.assertEffort(t, "low", "auto")
	f.selectEffort(t, "high")
	saved := loadTabsFile()
	if len(saved.Tabs) != 1 || saved.Tabs[0].Effort == nil || *saved.Tabs[0].Effort != "high" {
		t.Fatalf("selected effort was not persisted in the existing field: %+v", saved.Tabs)
	}
	f.app.CancelTab(f.tab.ID)
	waitNotRunning(t, old)
	f.assertEffort(t, "low", "high")
	if f.count.Load() != 1 || f.app.controllerForTab(f.tab) != old {
		t.Fatal("stopping applied the pending selection or started another task")
	}
	// Simulate the previous process exiting, then exercise the normal startup
	// builder using the persisted tab entry. No new persisted schema is needed.
	old.Close()
	f.tab.releaseSessionLease()
	restored := NewApp()
	restored.ctx = f.ctx
	restored.readyHook = func() {}
	entry := saved.Tabs[0]
	tab := &WorkspaceTab{ID: entry.ID, Scope: entry.Scope, WorkspaceRoot: entry.WorkspaceRoot,
		TopicID: entry.TopicID, SessionPath: entry.SessionPath, model: entry.Model, effort: cloneStringPtr(entry.Effort),
		disabledMCP: map[string]ServerView{}}
	restored.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	restored.tabOrder = []string{tab.ID}
	restored.activeTabID = tab.ID
	installNoopRuntimeEvents(restored, nil)
	restored.buildTabController(tab)
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})
	if tab.StartupErr != "" || tab.Ctrl == nil {
		t.Fatalf("restored tab failed to start: %s", tab.StartupErr)
	}
	info := restored.EffortForTab(tab.ID)
	if info.Current != "high" || info.Pending != nil {
		t.Fatalf("restored selection = %+v, want applied high", info)
	}
}

func TestEffortSelectionFailureRetainsRuntimeAndQueue(t *testing.T) {
	f := newEffortSelectionFixture(t)
	old := f.tab.Ctrl
	if err := f.app.SubmitToTab(f.tab.ID, "Say done."); err != nil {
		t.Fatal(err)
	}
	first := f.request(t, "low")
	f.selectEffort(t, "max")
	if err := f.app.SetInboxPaused(f.tab.ID, true); err != nil {
		t.Fatal(err)
	}
	receipt, err := f.app.EnqueueInboxFollowup(f.tab.ID, "queued", "queued", "effort-failure")
	if err != nil {
		t.Fatal(err)
	}
	first.reply <- effortFinalReply
	waitNotRunning(t, old)
	fail := errors.New("injected effort build failure")
	f.app.rebindCandidateHook = func(stage string) error {
		if stage == "effort_before_authority" {
			return fail
		}
		return nil
	}
	if _, _, err := f.app.beginTabTurn(f.tab.ID, false); !errors.Is(err, fail) {
		t.Fatalf("next-run failure = %v, want injected build error", err)
	}
	f.assertEffort(t, "low", "max")
	snapshot, err := f.app.InboxSnapshot(f.tab.ID)
	if err != nil || len(snapshot.Items) != 1 || snapshot.Items[0].ID != receipt.ItemID || snapshot.Items[0].State != "queued" {
		t.Fatalf("failed application consumed queued input: %+v %v", snapshot, err)
	}
	if f.app.controllerForTab(f.tab) != old || f.count.Load() != 1 {
		t.Fatal("failed application replaced or ran the old controller")
	}
	if err := old.Snapshot(); err != nil {
		t.Fatal("failed candidate invalidated the old controller's write authority:", err)
	}
	f.app.rebindCandidateHook = nil
	if err := f.app.SetInboxPaused(f.tab.ID, false); err != nil {
		t.Fatal(err)
	}
	next := f.request(t, "max")
	next.reply <- effortFinalReply
	waitNotRunning(t, f.app.controllerForTab(f.tab))
}

func TestEffortSelectionClearedByModelOrSessionChange(t *testing.T) {
	for _, action := range []string{"model", "new", "clear"} {
		t.Run(action, func(t *testing.T) {
			f := newEffortSelectionFixture(t)
			if err := f.app.SubmitToTab(f.tab.ID, "Say done."); err != nil {
				t.Fatal(err)
			}
			first := f.request(t, "low")
			f.selectEffort(t, "max")
			first.reply <- effortFinalReply
			waitNotRunning(t, f.tab.Ctrl)
			var cleared atomic.Int32
			f.tab.sink.SetBotSink(event.FuncSink(func(e event.Event) {
				if e.Code == "effort_selection_cleared" {
					cleared.Add(1)
				}
			}))
			var err error
			switch action {
			case "model":
				err = f.app.SetModelForTab(f.tab.ID, "effort-test/other")
			case "new":
				err = f.app.NewSessionForTab(f.tab.ID)
			case "clear":
				_, err = f.app.ClearSessionForTab(f.tab.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			f.assertEffort(t, "low", "")
			if cleared.Load() != 1 {
				t.Fatalf("cleared-selection notice count = %d, want one", cleared.Load())
			}
		})
	}
}

func TestEffortSelectionDoesNotSpoofCurrentWhenConfigChanges(t *testing.T) {
	f := newEffortSelectionFixture(t)
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Providers[0].Effort = "high"
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	f.assertEffort(t, "low", "")
}

func TestEffortSelectionPersistenceFailureRollsBack(t *testing.T) {
	f := newEffortSelectionFixture(t)
	if err := f.app.SubmitToTab(f.tab.ID, "Wait."); err != nil {
		t.Fatal(err)
	}
	request := f.request(t, "low")
	f.selectEffort(t, "high")
	tmp := filepath.Join(desktopConfigDir(), tabsFileName+".tmp")
	if err := os.Mkdir(tmp, 0700); err != nil {
		t.Fatal(err)
	}
	if err := f.app.SetEffortForTab(f.tab.ID, "max"); err == nil {
		t.Fatal("unpersisted selection was acknowledged")
	}
	f.assertEffort(t, "low", "high")
	if got := loadTabsFile().Tabs[0].Effort; got == nil || *got != "high" {
		t.Fatalf("saved target changed: %v", got)
	}
	if err := os.Remove(tmp); err != nil {
		t.Fatal(err)
	}
	request.reply <- effortFinalReply
	waitNotRunning(t, f.tab.Ctrl)
}

// A failure after the new controller is published must not leave the tab in a
// half-applied state: the swapped controller stays live and usable, the applied
// effort is visible immediately, and no pending selection survives.
func TestEffortSelectionPersistenceFailureAfterPublication(t *testing.T) {
	f := newEffortSelectionFixture(t)
	old := f.tab.Ctrl
	tmp := filepath.Join(desktopConfigDir(), tabsFileName+".tmp")
	t.Cleanup(func() { _ = os.Remove(tmp) })
	f.app.rebindCandidateHook = func(stage string) error {
		if stage == "effort_before_authority" {
			return os.Mkdir(tmp, 0o700)
		}
		return nil
	}
	err := f.app.SetEffortForTab(f.tab.ID, "max")
	if err == nil || !strings.Contains(err.Error(), "could not persist tab settings") {
		t.Fatalf("post-publication persistence error = %v, want tab settings failure", err)
	}
	f.app.rebindCandidateHook = nil
	if f.app.controllerForTab(f.tab) == old {
		t.Fatal("published controller was rolled back after the persistence failure")
	}
	f.assertEffort(t, "max", "")
	if got := loadTabsFile().Tabs[0].Effort; got == nil || *got != "max" {
		t.Fatalf("published effort was not saved: %v", got)
	}
	if err := f.tab.Ctrl.Snapshot(); err != nil {
		t.Fatal("published controller is not usable:", err)
	}
	if err := os.Remove(tmp); err != nil {
		t.Fatal(err)
	}
}

func TestEffortSelectionConcurrentRebuildLastAcceptedWins(t *testing.T) {
	f := newEffortSelectionFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var blocked atomic.Bool
	f.app.rebindCandidateHook = func(stage string) error {
		if stage == "effort_before_authority" && blocked.CompareAndSwap(false, true) {
			close(entered)
			select {
			case <-release:
			case <-f.ctx.Done():
				return f.ctx.Err()
			}
		}
		return nil
	}
	first := make(chan error, 1)
	go func() { first <- f.app.SetEffortForTab(f.tab.ID, "high") }()
	select {
	case <-entered:
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	last := make(chan error, 1)
	go func() { last <- f.app.SetEffortForTab(f.tab.ID, "max") }()
	f.assertEffort(t, "low", "high")
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-last; err != nil {
		t.Fatal(err)
	}
	f.assertEffort(t, "max", "")
	if err := f.tab.Ctrl.Snapshot(); err != nil {
		t.Fatal(err)
	}
}

func TestEffortSelectionFinishingTurnAdmission(t *testing.T) {
	f := newEffortSelectionFixture(t)
	finishing, release := make(chan struct{}), make(chan struct{})
	var blocked atomic.Bool
	f.tab.sink.SetBotSink(event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnDone && blocked.CompareAndSwap(false, true) {
			close(finishing)
			select {
			case <-release:
			case <-f.ctx.Done():
			}
		}
	}))
	if err := f.app.SubmitToTab(f.tab.ID, "First."); err != nil {
		t.Fatal(err)
	}
	first := f.request(t, "low")
	f.selectEffort(t, "max")
	first.reply <- effortFinalReply
	select {
	case <-finishing:
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	submitted := make(chan error, 1)
	go func() { submitted <- f.app.SubmitToTab(f.tab.ID, "Second.") }()
	close(release)
	if err := <-submitted; err != nil {
		t.Fatal(err)
	}
	next := f.request(t, "max")
	next.reply <- effortFinalReply
	waitNotRunning(t, f.app.controllerForTab(f.tab))
	if f.count.Load() != 2 {
		t.Fatal("finishing boundary submitted duplicate requests")
	}
}

func TestEffortSelectionFinalAuthorityFailureAndRecovery(t *testing.T) {
	f := newEffortSelectionFixture(t)
	old := f.tab.Ctrl
	var revoked atomic.Bool
	f.app.rebindCandidateHook = func(stage string) error {
		if stage == "effort_before_authority" && revoked.CompareAndSwap(false, true) {
			f.tab.sessionLeaseMu.Lock()
			lease := f.tab.sessionLease
			f.tab.sessionLeaseMu.Unlock()
			lease.Release()
		}
		return nil
	}
	if err := f.app.SetEffortForTab(f.tab.ID, "max"); !errors.Is(err, agent.ErrSessionWriteAuthorityStale) {
		t.Fatalf("expected revoked authority error, got %v", err)
	}
	f.assertEffort(t, "low", "max")
	if f.app.controllerForTab(f.tab) != old || f.count.Load() != 0 {
		t.Fatal("failed authority published or started the candidate")
	}
	f.selectEffort(t, "max")
	f.assertEffort(t, "max", "")
	if err := f.tab.Ctrl.Snapshot(); err != nil {
		t.Fatal("retry did not restore write authority:", err)
	}
}

func TestEffortSelectionBuildFailureRetainsSource(t *testing.T) {
	f := newEffortSelectionFixture(t)
	old := f.tab.Ctrl
	configPath := filepath.Join(f.tab.WorkspaceRoot, "reasonix.toml")
	if err := os.WriteFile(configPath, []byte("[agent]\nsystem_prompt_file = \"/outside-workspace/missing-effort-prompt.md\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.app.SetEffortForTab(f.tab.ID, "max"); err == nil {
		t.Fatal("invalid prompt did not fail boot")
	}
	if f.app.controllerForTab(f.tab) != old {
		t.Fatal("failed build replaced source")
	}
	f.assertEffort(t, "low", "max")
	if err := old.Snapshot(); err != nil {
		t.Fatal("source no longer writable:", err)
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	f.selectEffort(t, "max")
	f.assertEffort(t, "max", "")
}

func TestEffortSelectionIsScopedToTab(t *testing.T) {
	f := newEffortSelectionFixture(t)
	siblingCtrl, err := boot.Build(f.ctx, boot.Options{Model: "effort-test/m", WorkspaceRoot: t.TempDir(), Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sibling := &WorkspaceTab{ID: "sibling", Scope: "global", Ready: true, Ctrl: siblingCtrl, model: "effort-test/m", disabledMCP: map[string]ServerView{}}
	sibling.sink = &tabEventSink{tabID: sibling.ID, app: f.app}
	f.app.mu.Lock()
	f.app.tabs[sibling.ID] = sibling
	f.app.tabOrder = append(f.app.tabOrder, sibling.ID)
	f.app.mu.Unlock()
	t.Cleanup(func() {
		if ctrl := f.app.controllerForTab(sibling); ctrl != nil {
			ctrl.Close()
		}
		sibling.releaseSessionLease()
	})
	if err := f.app.SubmitToTab(f.tab.ID, "Wait."); err != nil {
		t.Fatal(err)
	}
	request := f.request(t, "low")
	f.app.mu.Lock()
	f.app.activeTabID = sibling.ID
	f.app.mu.Unlock()
	f.selectEffort(t, "max")
	f.assertEffort(t, "low", "max")
	if info := f.app.EffortForTab(sibling.ID); info.Current != "low" || info.Pending != nil {
		t.Fatalf("selection leaked to active sibling: %+v", info)
	}
	if err := f.app.SetEffort("high"); err != nil {
		t.Fatal("legacy active-tab setter:", err)
	}
	if info := f.app.Effort(); info.Current != "high" || info.Pending != nil {
		t.Fatalf("legacy getter = %+v", info)
	}
	f.assertEffort(t, "low", "max")
	request.reply <- effortFinalReply
	waitNotRunning(t, f.app.controllerForTab(f.tab))
}

func TestEffortSelectionSessionRebindClearsSource(t *testing.T) {
	f := newEffortSelectionFixture(t)
	if err := f.tab.Ctrl.Snapshot(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.tab.SessionPath)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(filepath.Dir(f.tab.SessionPath), "different-session.jsonl")
	if err := os.WriteFile(target, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.app.SubmitToTab(f.tab.ID, "Wait."); err != nil {
		t.Fatal(err)
	}
	request := f.request(t, "low")
	f.selectEffort(t, "max")
	request.reply <- effortFinalReply
	waitNotRunning(t, f.app.controllerForTab(f.tab))
	if _, err := f.app.ResumeSessionForTab(f.tab.ID, target); err != nil {
		t.Fatal(err)
	}
	f.assertEffort(t, "low", "")
	if got := f.app.currentSessionPathFor(f.tab); got != target {
		t.Fatalf("rebind path = %q", got)
	}
}
