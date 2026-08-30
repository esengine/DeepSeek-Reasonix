package main

import (
	"slices"
	"strings"
	"testing"

	"reasonix/internal/config"
)

// TestSetModelForTabRemoteCredentialPostsServeModel: remote-credential hosts
// switch through the serve's per-session endpoint.
func TestSetModelForTabRemoteCredentialPostsServeModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	cfg := config.Default()
	if err := cfg.UpsertRemoteHost(config.RemoteHostEntry{Name: "box", Host: "127.0.0.1", Port: 22, User: "dev"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	a.remoteTabMu.Lock()
	a.remoteTabLayout.activeID = "other-tab"
	a.remoteTabMu.Unlock()

	if err := a.SetModelForTab(meta.ID, "remote/chat"); err != nil {
		t.Fatalf("SetModelForTab: %v", err)
	}
	posted := false
	for _, c := range fs.recorded() {
		if strings.HasPrefix(c, "POST /model ") && strings.Contains(c, `"ref":"remote/chat"`) {
			posted = true
		}
	}
	if !posted {
		t.Fatalf("serve never saw POST /model with the ref: %v", fs.recorded())
	}
	a.remoteTabMu.Lock()
	model, activeID := "", a.remoteTabLayout.activeID
	if tab := a.remoteTabs[meta.ID]; tab != nil {
		model = tab.model
	}
	a.remoteTabMu.Unlock()
	if model != "remote/chat" {
		t.Fatalf("tab.model = %q, want remote/chat", model)
	}
	if activeID != "other-tab" {
		t.Fatalf("completed model switch reactivated %q, want other-tab to stay active", activeID)
	}
	if !slices.ContainsFunc(log.recorded(), func(event string) bool { return strings.HasPrefix(event, "remote-tab:updated ") }) {
		t.Fatalf("model switch did not publish metadata update: %v", log.recorded())
	}
}

// Remote-credential hosts must adopt the Serve catalog and active ref without
// briefly inheriting the desktop's configured default model.
func TestModelsForTabRemoteCredentialHostOffersServeCatalog(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "REMOTE_MODEL_TEST_KEY", "sk-test")
	cfg := config.Default()
	cfg.DefaultModel = "desktop/local-model"
	cfg.Desktop.ProviderAccess = append(cfg.Desktop.ProviderAccess, "desktop")
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{
		Name: "desktop", BaseURL: "https://desktop.invalid/v1", Models: []string{"local-model"},
		Default: "local-model", APIKeyEnv: "REMOTE_MODEL_TEST_KEY",
	})
	if err := cfg.UpsertRemoteHost(config.RemoteHostEntry{Name: "box", Host: "127.0.0.1", Port: 22, User: "dev"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	a.remoteTabMu.Lock()
	seeded := a.remoteTabs[meta.ID].model
	a.remoteTabMu.Unlock()
	if seeded != "" {
		t.Fatalf("remote-credential tab seeded desktop model %q", seeded)
	}
	if _, err := a.RemoteTabSnapshot(meta.ID); err != nil {
		t.Fatalf("RemoteTabSnapshot: %v", err)
	}
	a.remoteTabMu.Lock()
	adopted := a.remoteTabs[meta.ID].model
	a.remoteTabMu.Unlock()
	if adopted != "remote/chat" {
		t.Fatalf("remote-credential tab model = %q, want Serve active ref", adopted)
	}
	got := a.ModelsForTab(meta.ID)
	if len(got) != 1 || got[0].Ref != "remote/chat" || !got[0].Current {
		t.Fatalf("ModelsForTab = %+v, want the Serve catalog with remote/chat current", got)
	}
}

func TestSetRemoteTabModelFailureKeepsPreviousModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	cfg := config.Default()
	cfg.DefaultModel = "deepseek/deepseek-v4-flash"
	cfg.Desktop.ProviderAccess = []string{"deepseek"}
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{
		Name: "deepseek", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic",
		Models: []string{"deepseek-v4-flash", "deepseek-v4-pro"}, Default: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY",
	})
	if err := cfg.UpsertRemoteHost(config.RemoteHostEntry{Name: "box", Host: "127.0.0.1", Port: 22, User: "dev", CredentialMode: "local-proxy"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})

	// Clear the stored key after the tab seeds its default. The proxy switch
	// must then fail while preserving that model.
	if _, err := config.SetCredential("DEEPSEEK_API_KEY", ""); err != nil {
		t.Fatalf("clear credential: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "")
	if err := a.SetModelForTab(meta.ID, "deepseek/deepseek-v4-pro"); err == nil {
		t.Fatal("SetModelForTab must fail without the local key")
	}
	a.remoteTabMu.Lock()
	model := ""
	if tab := a.remoteTabs[meta.ID]; tab != nil {
		model = tab.model
	}
	a.remoteTabMu.Unlock()
	if model != "deepseek/deepseek-v4-flash" {
		t.Fatalf("tab.model = %q, want the untouched previous model", model)
	}
}
