package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSetModelFailureLeavesTabStateIntact(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte(`
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	tab := app.createTabEntryWithID("project", workspace, "", "tab1")
	app.tabs[tab.ID] = tab
	app.activeTabID = tab.ID
	app.buildTabController(tab)
	if tab.Ctrl == nil {
		t.Fatalf("tab controller failed to build: %s", tab.StartupErr)
	}
	defer tab.Ctrl.Close()

	oldCtrl := tab.Ctrl
	oldModel := tab.model

	if err := app.SetModel("no-such-provider/no-such-model"); err == nil {
		t.Fatal("SetModel with an unknown model should return an error")
	}
	if tab.Ctrl != oldCtrl {
		t.Fatal("a failed model switch must keep the existing controller, not replace/close it")
	}
	if tab.model != oldModel {
		t.Fatalf("tab.model changed to %q on a failed switch, want %q", tab.model, oldModel)
	}
}
