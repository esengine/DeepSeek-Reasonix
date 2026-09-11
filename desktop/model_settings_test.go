package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func modelSettingsBootTab(t *testing.T, app *App, id, root, model string) *WorkspaceTab {
	t.Helper()
	ctrl, err := boot.Build(app.ctx, boot.Options{Model: model, WorkspaceRoot: root, SessionDir: desktopSessionDir(root), Sink: event.Discard, BeforeInboxDispatch: app.beforeInboxDispatch})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ctrl.SessionDir(), id+".jsonl")
	history := append(ctrl.History(), provider.Message{Role: provider.RoleUser, Content: "keep history " + id})
	ctrl.AdoptHistory(history, path)
	tab := &WorkspaceTab{ID: id, Scope: "project", WorkspaceRoot: root, Ready: true, Ctrl: ctrl, model: model, SessionPath: path, disabledMCP: map[string]ServerView{}, sink: &tabEventSink{tabID: id, app: app}}
	if app.tabs == nil {
		app.tabs = map[string]*WorkspaceTab{}
	}
	app.tabs[id] = tab
	app.tabOrder = append(app.tabOrder, id)
	if err := app.saveTabSessionMeta(tab, path); err != nil {
		t.Fatal(err)
	}
	installNoopRuntimeEvents(app, tab.sink)
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})
	return tab
}

func TestModelSettingsRemovalRetryKeepsFailedTargetAndAppliesInactiveSibling(t *testing.T) {
	isolateDesktopUserDirs(t)
	oldRef, newRef := configureSwitchableDefaultModels(t)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	brokenRoot, workingRoot := t.TempDir(), t.TempDir()
	broken := modelSettingsBootTab(t, app, "broken", brokenRoot, oldRef)
	working := modelSettingsBootTab(t, app, "working", workingRoot, oldRef)
	app.activeTabID = broken.ID
	oldBroken, oldWorking := broken.Ctrl, working.Ctrl
	if err := os.WriteFile(filepath.Join(brokenRoot, "reasonix.toml"), []byte("[agent]\nsystem_prompt_file = \"/outside-workspace/prompt.md\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteProvider("old"); err != nil {
		t.Fatal(err)
	}
	result := app.RetryModelSettingsApplication(broken.ID)
	if result.Application != "failed" || broken.Ctrl != oldBroken || broken.model != oldRef {
		t.Fatalf("failed target was not preserved: %+v", result)
	}
	result = app.RetryModelSettingsApplication(working.ID)
	if working.Ctrl == oldWorking || working.model != newRef || broken.Ctrl != oldBroken {
		t.Fatal("retry did not target the inactive sibling")
	}
	for _, tab := range []*WorkspaceTab{broken, working} {
		if got := tab.Ctrl.History(); len(got) < 2 || got[1].Content != "keep history "+tab.ID {
			t.Fatalf("history lost for %s", tab.ID)
		}
	}
	for _, target := range result.Targets {
		if target.TabID == working.ID && target.Application != "applied" {
			t.Fatalf("sibling status: %+v", target)
		}
	}
	if admission, accepted, err := app.beginTabTurn(broken.ID, false); err == nil {
		admission.abort()
		applied, desired, stateErr := accepted.(modelSettingsSnapshot).ModelSettingsState()
		t.Fatalf("failed target accepted: old=%v model=%q rootMatches=%v fresh=%v stateErr=%v status=%+v", accepted == oldBroken, broken.model, broken.WorkspaceRoot == brokenRoot, applied == desired, stateErr, app.GetModelSettingsApplication())
	}
}

func TestModelSettingsProjectOverrideSkipsRebuild(t *testing.T) {
	isolateDesktopUserDirs(t)
	oldRef, newRef := configureSwitchableDefaultModels(t)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "reasonix.toml"), []byte("[agent]\nplanner_model = \""+oldRef+"\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	project := modelSettingsBootTab(t, app, "project", projectRoot, oldRef)
	global := modelSettingsBootTab(t, app, "global", t.TempDir(), oldRef)
	app.activeTabID = project.ID
	oldProject, oldGlobal := project.Ctrl, global.Ctrl
	if err := app.SetPlannerModel(newRef); err != nil {
		t.Fatal(err)
	}
	for _, tab := range []*WorkspaceTab{project, global} {
		admission, _, err := app.beginTabTurn(tab.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		admission.abort()
	}
	if project.Ctrl != oldProject || global.Ctrl == oldGlobal {
		t.Fatal("effective project override was not honored at run admission")
	}
}

func TestModelSettingsRetryAppliesDetachedRuntimeWithoutCreatingTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	oldRef, newRef := configureSwitchableDefaultModels(t)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	tab := modelSettingsBootTab(t, app, "background", t.TempDir(), oldRef)
	if !app.detachSessionRuntime(tab) {
		t.Fatal("detach runtime")
	}
	delete(app.tabs, tab.ID)
	app.tabOrder = nil
	app.activeTabID = ""
	old := tab.Ctrl
	if err := app.SetPlannerModel(newRef); err != nil {
		t.Fatal(err)
	}
	result := app.RetryModelSettingsApplication(tab.ID)
	if result.Application != "applied" || tab.Ctrl == old || len(app.tabs) != 0 || app.activeTabID != "" {
		t.Fatalf("detached application created a tab or failed: %+v", result)
	}
	if history := tab.Ctrl.History(); len(history) < 2 || history[1].Content != "keep history background" {
		t.Fatal("detached application lost history")
	}
	current := tab.Ctrl
	if result := app.RetryModelSettingsApplication(tab.ID); result.Application != "applied" || tab.Ctrl != current {
		t.Fatal("unchanged detached runtime rebuilt again")
	}
}

func TestModelSettingsQueuedFollowupAppliesLatestBeforeDispatch(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	keys := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		keys <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	app := NewApp()
	view := ProviderView{Name: "queued", Kind: "openai", BaseURL: server.URL, Models: []string{"m"}, NoProxy: true}
	if _, err := app.SaveProviderWithKey(view, "old-queued-key"); err != nil {
		t.Fatal(err)
	}
	app.ctx = ctx
	app.readyHook = func() {}
	tab := modelSettingsBootTab(t, app, "queued", t.TempDir(), "queued/m")
	app.activeTabID = tab.ID
	old := tab.Ctrl.(*control.Controller)
	if err := old.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	if _, err := old.TryEnqueueFollowup(control.InboxRequest{Display: "queued prompt", Submit: "queued prompt", Idempotency: "queued-settings-test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveProviderWithKey(view, "new-queued-key"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{}, 1)
	tab.sink.SetBotSink(event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnDone {
			done <- struct{}{}
		}
	}))
	if err := old.SetInboxPaused(false); err != nil {
		t.Fatal(err)
	}
	select {
	case key := <-keys:
		if key != "Bearer new-queued-key" {
			t.Fatal("queued message used the retired connection")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if app.controllerForTab(tab) == old {
		t.Fatal("queued message did not replace the stale runtime")
	}
}

func TestModelSettingsLastProviderRemovalBlocksNewRun(t *testing.T) {
	isolateDesktopUserDirs(t)
	oldRef, _ := configureSwitchableDefaultModels(t)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	tab := modelSettingsBootTab(t, app, "last", t.TempDir(), oldRef)
	app.activeTabID = tab.ID
	old := tab.Ctrl
	for _, name := range []string{"old", "new"} {
		if err := app.DeleteProvider(name); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := app.beginTabTurn(tab.ID, false); err == nil || !strings.Contains(err.Error(), "no configured model") {
		t.Fatalf("no-model admission = %v", err)
	}
	if tab.Ctrl != old || len(tab.Ctrl.History()) < 2 {
		t.Fatal("blocked admission destroyed current runtime/history")
	}
}

func TestModelSettingsStartupPublicationRejectsCandidateBuiltBeforeSave(t *testing.T) {
	isolateDesktopUserDirs(t)
	oldRef, newRef := configureSwitchableDefaultModels(t)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	app.stopDeferredRebuildRetry()
	tab := &WorkspaceTab{ID: "startup", Scope: "project", WorkspaceRoot: t.TempDir(), model: oldRef, disabledMCP: map[string]ServerView{}, sink: &tabEventSink{tabID: "startup", app: app}}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	installNoopRuntimeEvents(app, tab.sink)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	previousHook := sessionLeaseAcquireHookForTest
	sessionLeaseAcquireHookForTest = func() { once.Do(func() { close(entered); <-release }) }
	t.Cleanup(func() {
		sessionLeaseAcquireHookForTest = previousHook
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})
	go func() { defer close(done); app.buildTabController(tab) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("startup never reached lease publication barrier")
	}
	if err := app.SetPlannerModel(newRef); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("startup did not settle after save")
	}
	if tab.Ctrl != nil || !tab.modelApplication.startupRetry {
		t.Fatalf("stale candidate published: controller=%T error=%q", tab.Ctrl, tab.StartupErr)
	}
	sessionLeaseAcquireHookForTest = previousHook
	app.buildTabController(tab)
	if tab.Ctrl == nil {
		t.Fatalf("latest startup failed: %s", tab.StartupErr)
	}
	if stale, err := modelSettingsNeedApply(tab.Ctrl); err != nil || stale {
		t.Fatalf("replacement did not use latest settings: %v %v", stale, err)
	}
}

func TestModelSettingsGroupedCredentialCommitAndReceipt(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "GROUP_OLD_KEY", "old-key")
	cfg := config.Default()
	cfg.Providers = nil
	for _, name := range []string{"first", "second", "unrelated"} {
		cfg.Providers = append(cfg.Providers, config.ProviderEntry{Name: name, Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "chat", APIKeyEnv: "GROUP_OLD_KEY"})
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	key := "new-key"
	change := ModelSettingsChange{Kind: "credential", Names: []string{"first", "second"}, Key: &key, RequestID: "group-save", ExpectedFingerprint: app.Settings().ModelSettingsFingerprint}
	result := app.ApplyModelSettings(change)
	if !result.Persisted || result.Application != "not_required" || len(app.tabs) != 0 {
		t.Fatalf("group save without session: %+v", result)
	}
	saved := config.LoadForEdit(config.UserConfigPath())
	first, _ := saved.Provider("first")
	second, _ := saved.Provider("second")
	unrelated, _ := saved.Provider("unrelated")
	if first.APIKeyEnv != second.APIKeyEnv || first.APIKeyEnv == unrelated.APIKeyEnv || !first.Configured() || unrelated.APIKeyEnv != "GROUP_OLD_KEY" {
		t.Fatal("credential group did not switch together or changed unrelated connection")
	}
	before, _ := os.ReadFile(config.UserCredentialsPath())
	replay := app.ApplyModelSettings(change)
	receipt := app.GetModelSettingsRequest(change.RequestID)
	after, _ := os.ReadFile(config.UserCredentialsPath())
	if !replay.Persisted || !receipt.Persisted || replay.Revision != result.Revision || string(before) != string(after) {
		t.Fatal("replayed save wrote another credential or lost its receipt")
	}
	key = ""
	change.RequestID, change.ExpectedFingerprint = "group-clear", app.Settings().ModelSettingsFingerprint
	result = app.ApplyModelSettings(change)
	if !result.Persisted {
		t.Fatalf("clear: %+v", result)
	}
	saved = config.LoadForEdit(config.UserConfigPath())
	first, _ = saved.Provider("first")
	second, _ = saved.Provider("second")
	unrelated, _ = saved.Provider("unrelated")
	if first.Configured() || second.Configured() || !unrelated.Configured() {
		t.Fatal("clearing group did not preserve unrelated credential")
	}
}

func TestModelSettingsFailedCommitCleansOnlyItsStagedCredential(t *testing.T) {
	for _, failBeforeSave := range []bool{false, true} {
		t.Run(fmt.Sprint(failBeforeSave), func(t *testing.T) {
			isolateDesktopUserDirs(t)
			configureSwitchableDefaultModels(t)
			setDesktopTestCredential(t, "UNRELATED_KEY", "keep-me")
			path := config.UserConfigPath()
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var staged string
			injected := errors.New("injected configuration commit failure")
			_, err = NewApp().applyModelConfigChangeWithSave("test credential commit", func(c *config.Config) error {
				var stageErr error
				staged, stageErr = c.StageModelCredentialLocked("new-secret")
				if stageErr != nil {
					return stageErr
				}
				c.Providers[0].APIKeyEnv = staged
				if failBeforeSave {
					return injected
				}
				return nil
			}, func(*config.Config, string) error { return injected })
			if !errors.Is(err, injected) {
				t.Fatalf("failure = %v", err)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil || string(before) != string(after) {
				t.Fatal("failed edit modified committed config")
			}
			if staged == "" || config.CredentialStored(staged) || !config.CredentialStored("UNRELATED_KEY") {
				t.Fatal("failed edit did not clean only its new credential")
			}
		})
	}
}

func TestModelSettingsCredentialWriteFailureKeepsConfig(t *testing.T) {
	isolateDesktopUserDirs(t)
	configureSwitchableDefaultModels(t)
	before, _ := os.ReadFile(config.UserConfigPath())
	if err := os.Remove(config.UserCredentialsPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.UserCredentialsPath(), 0700); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	key := "secret"
	cfg := config.LoadForEdit(config.UserConfigPath())
	result := app.ApplyModelSettings(ModelSettingsChange{Kind: "credential", Name: cfg.Providers[0].Name, Key: &key, RequestID: "credential-failure", ExpectedFingerprint: app.Settings().ModelSettingsFingerprint})
	after, _ := os.ReadFile(config.UserConfigPath())
	if result.Persisted || len(result.Issues) == 0 || string(before) != string(after) {
		t.Fatalf("credential write failure: %+v", result)
	}
}

func TestModelSettingsSaveWithoutActiveSession(t *testing.T) {
	isolateDesktopUserDirs(t)
	_, ref := configureSwitchableDefaultModels(t)
	app := NewApp()
	app.ctx = context.Background()
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"default", func() error { return app.SetDefaultModel(ref) }},
		{"planner", func() error { return app.SetPlannerModel(ref) }},
		{"vision", func() error { return app.SetVisionModel("") }},
		{"search", func() error { return app.SetWebSearchModel("auto") }},
		{"subagent", func() error { return app.SetSubagentModel(ref) }},
		{"effort", func() error { return app.SetSubagentEffort("auto") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err != nil {
				t.Fatal(err)
			}
		})
	}
	if len(app.tabs) != 0 || app.activeTabID != "" {
		t.Fatal("saving created a session")
	}
	if config.LoadForEdit(config.UserConfigPath()).DefaultModel != ref {
		t.Fatal("default not persisted")
	}
}

func TestModelSettingsRequestRejectsStaleEdit(t *testing.T) {
	isolateDesktopUserDirs(t)
	oldRef, ref := configureSwitchableDefaultModels(t)
	app := NewApp()
	if err := app.SetDefaultModel(oldRef); err != nil {
		t.Fatal(err)
	}
	fingerprint := app.Settings().ModelSettingsFingerprint
	// Wails crosses JSON in both directions. Binary HMAC strings would be
	// replaced with U+FFFD, causing every real browser submission to conflict.
	wire, err := json.Marshal(fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip string
	if err := json.Unmarshal(wire, &roundTrip); err != nil || roundTrip != fingerprint {
		t.Fatalf("fingerprint is not JSON stable: %v", err)
	}
	first := app.ApplyModelSettings(ModelSettingsChange{Kind: "preference", Field: "default", Ref: ref, RequestID: "first", ExpectedFingerprint: fingerprint})
	if !first.Persisted {
		t.Fatalf("save: %+v", first.Issues)
	}
	second := app.ApplyModelSettings(ModelSettingsChange{Kind: "preference", Field: "default", Ref: oldRef, RequestID: "second", ExpectedFingerprint: fingerprint})
	if second.Persisted || len(second.Issues) == 0 {
		t.Fatal("stale edit accepted")
	}
	if config.LoadForEdit(config.UserConfigPath()).DefaultModel != ref {
		t.Fatal("stale edit overwrote committed config")
	}
	encoded, err := json.Marshal(first)
	if err != nil || strings.Contains(string(encoded), ":null") {
		t.Fatalf("array contract: %s %v", encoded, err)
	}
}

func TestModelSettingsRunningToolContinuationKeepsOldConnection(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("test input"), 0600); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var calls atomic.Int32
	auth := make(chan string, 8)
	oldServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		auth <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"read-1\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"input.txt\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(oldServer.Close)
	newAuth := make(chan string, 8)
	newServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		newAuth <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"new\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(newServer.Close)
	app := NewApp()
	view := ProviderView{Name: "snapshot-test", Kind: "openai", BaseURL: oldServer.URL, Models: []string{"m"}, NoProxy: true}
	if _, err := app.SaveProviderWithKey(view, "old-credential"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	old, err := boot.Build(ctx, boot.Options{Model: "snapshot-test/m", WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	app.ctx = ctx
	app.readyHook = func() {}
	app.setTestCtrl(old, "snapshot-test/m")
	tab := app.activeTab()
	tab.WorkspaceRoot = root
	path := filepath.Join(old.SessionDir(), "snapshot-test.jsonl")
	old.AdoptHistory(old.History(), path)
	tab.SessionPath = path
	installNoopRuntimeEvents(app, tab.sink)
	t.Cleanup(func() {
		once.Do(func() { close(release) })
		cancel()
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})
	if needed, err := modelSettingsNeedApply(old); err != nil || needed {
		t.Fatalf("fresh boot stale: %v %v", needed, err)
	}
	done := make(chan error, 1)
	go func() { done <- old.RunTurn(ctx, "Read input.txt") }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	view.BaseURL = newServer.URL
	if _, err := app.SaveProviderWithKey(view, "new-credential"); err != nil {
		t.Fatal(err)
	}
	if app.activeCtrl() != old || !old.RuntimeStatus().Running {
		t.Fatal("save replaced or stopped accepted work")
	}
	if status := app.GetModelSettingsApplication(); status.Application != "pending" {
		t.Fatalf("status: %+v", status)
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected tool continuation, got %d requests", calls.Load())
	}
	for range 2 {
		if got := <-auth; got != "Bearer old-credential" {
			t.Fatalf("current work switched key: %q", got)
		}
	}
	admission, current, err := app.beginTabTurn(tab.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	admission.abort()
	if current == old {
		t.Fatal("next run retained stale controller")
	}
	admission, same, err := app.beginTabTurn(tab.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	admission.abort()
	if same != current {
		t.Fatal("unchanged config rebuilt a second time")
	}
	if err := current.(*control.Controller).RunTurn(ctx, "Next run"); err != nil {
		t.Fatal(err)
	}
	if got := <-newAuth; got != "Bearer new-credential" {
		t.Fatalf("next run key: %q", got)
	}
}

func TestModelSettingsSaveDoesNotTouchRunningController(t *testing.T) {
	isolateDesktopUserDirs(t)
	_, ref := configureSwitchableDefaultModels(t)
	app := NewApp()
	app.ctx = context.Background()
	old := control.New(control.Options{Sink: event.Discard})
	t.Cleanup(old.Close)
	tab := &WorkspaceTab{ID: "running", Ctrl: old, Ready: true}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.activeTabID = tab.ID
	if err := app.SetPlannerModel(ref); err != nil {
		t.Fatal(err)
	}
	if app.controllerForTab(tab) != old {
		t.Fatal("saving replaced the current controller")
	}
}

func TestModelSettingsValidationDoesNotWriteCredential(t *testing.T) {
	isolateDesktopUserDirs(t)
	_, _ = configureSwitchableDefaultModels(t)
	before, _ := os.ReadFile(config.UserCredentialsPath())
	app := NewApp()
	if _, err := app.SaveProviderWithKey(ProviderView{Name: "", Kind: "invalid"}, "test-not-a-real-key"); err == nil {
		t.Fatal("invalid provider accepted")
	}
	after, _ := os.ReadFile(config.UserCredentialsPath())
	if string(before) != string(after) {
		t.Fatal("validation failure wrote credentials")
	}
}

func TestModelSettingsApplyFailureKeepsSavedConfiguration(t *testing.T) {
	isolateDesktopUserDirs(t)
	_, ref := configureSwitchableDefaultModels(t)
	app := NewApp()
	fingerprint := app.Settings().ModelSettingsFingerprint
	r := app.ApplyModelSettings(ModelSettingsChange{Kind: "preference", Field: "planner", Ref: ref, RequestID: "save", ExpectedFingerprint: fingerprint})
	if !r.Persisted {
		t.Fatalf("save: %+v", r.Issues)
	}
	retry := app.RetryModelSettingsApplication("closed-session")
	if !retry.Persisted || retry.Application != "failed" {
		t.Fatalf("retry: %+v", retry)
	}
	if config.LoadForEdit(config.UserConfigPath()).Agent.PlannerModel != ref {
		t.Fatal("retry altered saved configuration")
	}
}
