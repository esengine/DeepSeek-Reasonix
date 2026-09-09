package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reasonix/internal/config"
	"strings"
	"testing"
)

func TestWebSearchModelSettingsCandidatesAndStaleRef(t *testing.T) {
	isolateDesktopUserDirs(t)
	on := true
	cfg := &config.Config{Providers: []config.ProviderEntry{
		{Name: "search", Kind: "responses", BaseURL: "http://localhost:1234", Models: []string{"m", "org/fast"}, Default: "m", WebSearch: &on},
		{Name: "hidden", Kind: "responses", BaseURL: "http://localhost:1234", Model: "m", WebSearch: &on},
	}, Desktop: config.DesktopConfig{ProviderAccess: []string{"search"}}, Agent: config.AgentConfig{WebSearchModel: "missing/m"}}
	a := NewApp()
	var v SettingsView
	a.populateWebSearchSettings(&v, cfg, t.TempDir())
	if len(v.WebSearchModels) != 2 || v.WebSearchModelStatus != "invalid" {
		t.Fatalf("view: %+v", v)
	}
	if cfg.Agent.WebSearchModel != "missing/m" {
		t.Fatal("invalid ref cleared")
	}
	raw, _ := json.Marshal(a.defaultSettingsView())
	if !strings.Contains(string(raw), `"webSearchModels":[]`) {
		t.Fatal("fallback array is null")
	}
}

func TestWebSearchModelProjectOverrideView(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[agent]\nweb_search_model = \"project/m\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Agent.WebSearchModel = "global/m"
	v := SettingsView{WebSearchModel: cfg.Agent.WebSearchModel}
	NewApp().populateWebSearchSettings(&v, cfg, root)
	if !v.WebSearchModelOverridden || v.EffectiveWebSearchModel != "project/m" || v.WebSearchModel != "global/m" {
		t.Fatalf("override not explained: %+v", v)
	}
}

func TestSetWebSearchModelPersistsAndRebuilds(t *testing.T) {
	isolateDesktopUserDirs(t)
	on := true
	cfg := config.Default()
	cfg.Providers = []config.ProviderEntry{{Name: "local", Kind: "responses", BaseURL: "http://localhost:12345", Models: []string{"main", "search"}, Default: "main", WebSearch: &on}}
	cfg.DefaultModel = "local/main"
	cfg.Desktop.ProviderAccess = []string{"local"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	tab := testTab("search-assignment", globalWorkspaceRoot())
	tab.Scope = "global"
	tab.model = "local/main"
	tab.sink = &tabEventSink{tabID: tab.ID, app: a}
	a.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	a.tabOrder = []string{tab.ID}
	a.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
	})
	if err := a.SetWebSearchModel("local/search"); err != nil {
		t.Fatal(err)
	}
	if tab.Ctrl == nil {
		t.Fatal("controller not rebuilt")
	}
	if tab.model != "local/main" {
		t.Fatal("search changed conversation model")
	}
	raw, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `web_search_model = "local/search"`) {
		t.Fatal("assignment not saved")
	}
	if err := a.SetWebSearchModel("missing/model"); err == nil {
		t.Fatal("invalid assignment saved")
	}
	after, _ := os.ReadFile(config.UserConfigPath())
	if string(raw) != string(after) {
		t.Fatal("rejected assignment changed config")
	}
	if err := renameProviderConnections(cfg, []string{"local"}, "Renamed connection"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.ResolveWebSearchModel("local/search"); err != nil {
		t.Fatal("display rename invalidated assignment")
	}
}
