package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/control"
)

func testTab(id, root string) *WorkspaceTab {
	return &WorkspaceTab{
		ID:            id,
		Scope:         "project",
		WorkspaceRoot: root,
		Ready:         true,
		Ctrl:          control.New(control.Options{Label: id}),
		model:         "deepseek-flash/deepseek-v4-flash",
		mode:          "normal",
		disabledMCP:   map[string]ServerView{},
	}
}

func TestSetEffortForTabIsTabLocal(t *testing.T) {
	isolateDesktopUserDirs(t)

	rootA := t.TempDir()
	rootB := t.TempDir()
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	tabA := testTab("a", rootA)
	tabB := testTab("b", rootB)
	tabA.sink = &tabEventSink{tabID: tabA.ID, app: app}
	tabB.sink = &tabEventSink{tabID: tabB.ID, app: app}
	app.tabs = map[string]*WorkspaceTab{tabA.ID: tabA, tabB.ID: tabB}
	app.tabOrder = []string{tabA.ID, tabB.ID}
	app.activeTabID = tabA.ID
	defer func() {
		for _, tab := range app.tabs {
			if tab.Ctrl != nil {
				tab.Ctrl.Close()
			}
		}
	}()

	if err := app.SetEffortForTab(tabA.ID, "max"); err != nil {
		t.Fatalf("SetEffortForTab: %v", err)
	}
	if got := app.EffortForTab(tabA.ID).Current; got != "max" {
		t.Fatalf("tab A effort = %q, want max", got)
	}
	if got := app.EffortForTab(tabB.ID).Current; got != "auto" {
		t.Fatalf("tab B effort = %q, want auto", got)
	}
	if tabB.effort != nil {
		t.Fatalf("tab B stored effort = %q, want nil", *tabB.effort)
	}
	body, err := os.ReadFile(userConfigPathForTest())
	if err == nil && strings.Contains(string(body), `effort`) {
		t.Fatalf("tab-local effort should not write provider config:\n%s", body)
	}
}

func TestEffortForTabResolvesProjectProviderConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	configBody := `default_model = "project-provider/deepseek-v4-flash"
[[providers]]
name = "project-provider"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "PROJECT_API_KEY"
effort = "max"
`
	if err := os.WriteFile(filepath.Join(projectRoot, "reasonix.toml"), []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	tab := testTab("project", projectRoot)
	tab.model = "project-provider/deepseek-v4-flash"
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.activeTabID = tab.ID
	defer tab.Ctrl.Close()

	got := app.EffortForTab(tab.ID)
	if !got.Supported || got.Current != "max" {
		t.Fatalf("EffortForTab project config = %+v, want supported max", got)
	}
}

func TestEffortForTabUsesKnownModelRegistry(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	configBody := `default_model = "project-provider/deepseek-v4-flash"
[[providers]]
name = "project-provider"
kind = "openai"
base_url = "https://proxy.example.com/v1"
model = "deepseek-v4-flash"
api_key_env = "PROJECT_API_KEY"
`
	if err := os.WriteFile(filepath.Join(projectRoot, "reasonix.toml"), []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	tab := testTab("project", projectRoot)
	tab.model = "project-provider/deepseek-v4-flash"
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.activeTabID = tab.ID
	defer tab.Ctrl.Close()

	got := app.EffortForTab(tab.ID)
	if !got.Supported || got.Current != "auto" || got.Default != "high" {
		t.Fatalf("EffortForTab model registry = %+v, want supported auto/high", got)
	}
	wantLevels := []string{"auto", "high", "max"}
	if len(got.Levels) != len(wantLevels) {
		t.Fatalf("levels = %v, want %v", got.Levels, wantLevels)
	}
	for i, want := range wantLevels {
		if got.Levels[i] != want {
			t.Fatalf("levels[%d] = %q, want %q", i, got.Levels[i], want)
		}
	}
}

func TestSaveTabsPersistsModelAndEffort(t *testing.T) {
	isolateDesktopUserDirs(t)

	effort := "max"
	app := NewApp()
	tab := testTab("a", t.TempDir())
	tab.effort = &effort
	tab.model = "deepseek/deepseek-v4-pro"
	tab.mode = "plan"
	tab.Ctrl.SetPlanMode(true)
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	app.mu.Lock()
	app.saveTabsLocked()
	app.mu.Unlock()

	got := loadTabsFile()
	if len(got.Tabs) != 1 {
		t.Fatalf("tabs len = %d, want 1", len(got.Tabs))
	}
	if got.Tabs[0].Model != tab.model {
		t.Fatalf("saved model = %q, want %q", got.Tabs[0].Model, tab.model)
	}
	if got.Tabs[0].Effort == nil || *got.Tabs[0].Effort != effort {
		t.Fatalf("saved effort = %#v, want %q", got.Tabs[0].Effort, effort)
	}
	if got.Tabs[0].Mode != "plan" {
		t.Fatalf("saved mode = %q, want plan", got.Tabs[0].Mode)
	}
}

func TestSaveTabsDoesNotPersistYoloMode(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	tab := testTab("a", t.TempDir())
	tab.mode = "yolo"
	tab.Ctrl.SetBypass(true)
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	app.mu.Lock()
	app.saveTabsLocked()
	app.mu.Unlock()

	got := loadTabsFile()
	if len(got.Tabs) != 1 {
		t.Fatalf("tabs len = %d, want 1", len(got.Tabs))
	}
	if got.Tabs[0].Mode != "" {
		t.Fatalf("saved yolo mode = %q, want empty", got.Tabs[0].Mode)
	}
}

func userConfigPathForTest() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return dir + "/reasonix/reasonix.toml"
	}
	return ""
}
