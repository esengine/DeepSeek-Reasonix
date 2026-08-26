package main

import (
	"testing"

	"reasonix/internal/config"
)

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
