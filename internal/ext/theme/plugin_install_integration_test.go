package theme_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/ext/theme"
)

type themeInstallAction struct {
	Kind       string `json:"kind"`
	ThemeCount int    `json:"themeCount"`
	SkillCount int    `json:"skillCount"`
	HookCount  int    `json:"hookCount"`
	ToolCount  int    `json:"toolCount"`
}

type themeInstallResult struct {
	Status  string               `json:"status"`
	Applied bool                 `json:"applied"`
	Actions []themeInstallAction `json:"actions"`
}

func TestThemeOnlyPluginInstallDiscoverDisableAndRemove(t *testing.T) {
	home := testenv.TempDir(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	source := filepath.Join(testenv.TempDir(t), "palette")
	for path, body := range map[string]string{
		pluginpkg.NativeManifest:  `{"apiVersion":"reasonix.io/plugin/v2","name":"palette","version":"1.0.0","contributes":{"themes":["themes/*/theme.json"]}}`,
		"themes/night/theme.json": `{"schemaVersion":1,"name":"Night","author":"fixture","tokens":{"light":{"bg":"#FFFFFF","fg":"#111111"},"dark":{"bg":"#000000","fg":"#EEEEEE"}}}`,
	} {
		full := filepath.Join(source, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	installer := installsource.NewTool(installsource.Options{ProjectRoot: testenv.TempDir(t), HomeDir: home})
	run := func(apply bool) themeInstallResult {
		t.Helper()
		args, err := json.Marshal(map[string]any{"source": source, "kind": "plugin", "apply": apply})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := installer.Execute(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		var result themeInstallResult
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	preview := run(false)
	if preview.Status != "planned" || preview.Applied || len(preview.Actions) != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	action := preview.Actions[0]
	if action.Kind != "plugin" || action.ThemeCount != 1 || action.SkillCount != 0 || action.HookCount != 0 || action.ToolCount != 0 {
		t.Fatalf("theme-only preview advertises unexpected capabilities: %+v", action)
	}
	if done := run(true); done.Status != "done" || !done.Applied {
		t.Fatalf("install = %+v", done)
	}

	const id = "plugin:palette:night"
	visible := func() bool {
		for _, pack := range theme.List() {
			if pack.ID == id {
				return true
			}
		}
		return false
	}
	if !visible() {
		t.Fatalf("installed theme %s was not discovered", id)
	}
	if pack, err := theme.Load(id); err != nil || pack.Name != "Night" {
		t.Fatalf("load installed theme = %+v, %v", pack, err)
	}
	if err := pluginpkg.SetEnabled(reasonixHome, "palette", false); err != nil {
		t.Fatal(err)
	}
	if visible() {
		t.Fatal("disabled theme remains discoverable")
	}
	if _, err := theme.Load(id); err == nil {
		t.Fatal("disabled theme still loads")
	}
	if err := pluginpkg.SetEnabled(reasonixHome, "palette", true); err != nil || !visible() {
		t.Fatalf("re-enabled theme not discoverable: %v", err)
	}
	if _, ok, err := pluginpkg.Remove(reasonixHome, "palette"); err != nil || !ok {
		t.Fatalf("remove plugin: ok=%v err=%v", ok, err)
	}
	if visible() {
		t.Fatal("removed theme remains discoverable")
	}
}
