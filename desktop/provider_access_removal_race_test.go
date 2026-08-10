package main

import (
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

func TestRemoveProviderAccessRejectsCustomProviderThatBecameOfficialDuringSnapshot(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	setDesktopTestCredential(t, "MIMO_API_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "deepseek-flash/deepseek-v4-flash"
	cfg.Desktop.ProviderAccess = []string{"deepseek-flash", "mimo-pro"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://proxy.example/v1", Model: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY"},
		{Name: "mimo-pro", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5-pro", APIKeyEnv: "MIMO_API_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	ctrl := newBlockingSnapshotCtrl(control.New(control.Options{Label: "custom-deepseek"}))
	tab := &WorkspaceTab{ID: "custom-deepseek", Scope: "global", model: cfg.DefaultModel, Ctrl: ctrl}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	done := make(chan error, 1)
	go func() { done <- app.RemoveProviderAccess("deepseek-flash") }()
	<-ctrl.firstSnapshotStarted

	unlock := config.LockUserConfigEdits()
	changed := config.LoadForEdit(config.UserConfigPath())
	provider, ok := changed.Provider("deepseek-flash")
	if !ok {
		unlock()
		t.Fatal("deepseek-flash provider missing")
	}
	provider.BaseURL = "https://api.deepseek.com/anthropic"
	provider.Kind = "anthropic"
	if err := changed.SaveTo(config.UserConfigPath()); err != nil {
		unlock()
		t.Fatalf("save overlapping config edit: %v", err)
	}
	unlock()
	close(ctrl.releaseSnapshot)

	if err := <-done; err == nil {
		t.Fatal("RemoveProviderAccess deleted a custom provider that became official during snapshot")
	}
	got := config.LoadForEdit(config.UserConfigPath())
	provider, ok = got.Provider("deepseek-flash")
	if !ok || officialProviderKindFromEntry(*provider) != "deepseek" {
		t.Fatalf("updated official provider was not preserved: %+v/%v", provider, ok)
	}
	access := providerAccessSet(got.Desktop.ProviderAccess)
	if !access["deepseek"] && !access["deepseek-flash"] {
		t.Fatalf("provider access changed after rejected overlap: %+v", got.Desktop.ProviderAccess)
	}
	if ctrl.closeCount.Load() != 0 || tab.Ctrl != ctrl || tab.model != cfg.DefaultModel {
		t.Fatalf("runtime mutated after rejected overlap: closes=%d ctrl=%T model=%q", ctrl.closeCount.Load(), tab.Ctrl, tab.model)
	}
}
