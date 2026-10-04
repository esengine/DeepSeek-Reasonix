package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/ext/theme"
)

func TestStudioThemeAuthorExampleLifecycle(t *testing.T) {
	home := testenv.TempDir(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	t.Setenv("REASONIX_STATE_HOME", reasonixHome)
	source := filepath.Join(testenv.TempDir(t), "theme-example")
	if err := os.CopyFS(source, os.DirFS(filepath.Join("..", "..", "..", "docs", "themes"))); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(source, "paper-dawn", "theme.json"))
	if err != nil {
		t.Fatal(err)
	}
	var authored struct {
		Name   string                       `json:"name"`
		Tokens map[string]map[string]string `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &authored); err != nil {
		t.Fatal(err)
	}
	installer := installsource.NewTool(installsource.Options{
		ProjectRoot: testenv.TempDir(t), HomeDir: home, RequireApprovedPlan: true,
	})
	type installResult struct {
		OK      bool   `json:"ok"`
		Status  string `json:"status"`
		Applied bool   `json:"applied"`
		PlanID  string `json:"planId"`
		Actions []struct {
			Target       string `json:"target"`
			ThemeCount   int    `json:"themeCount"`
			SkillCount   int    `json:"skillCount"`
			AgentCount   int    `json:"agentCount"`
			CommandCount int    `json:"commandCount"`
			PromptCount  int    `json:"promptCount"`
			HookCount    int    `json:"hookCount"`
			ToolCount    int    `json:"toolCount"`
			Runtime      any    `json:"runtime"`
		} `json:"actions"`
	}
	run := func(args map[string]any) installResult {
		t.Helper()
		input, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		output, err := installer.Execute(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		var result installResult
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("install response %q: %v", output, err)
		}
		if !result.OK {
			t.Fatalf("install response: %s", output)
		}
		return result
	}
	const id = "plugin:paper-dawn-kit:paper-dawn"
	srv := themeServer(t)
	check := func(stage string, present, active bool) {
		t.Helper()
		t.Run(stage, func(t *testing.T) {
			response, err := http.Get(srv.URL + "/themes")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("theme listing = %d", response.StatusCode)
			}
			var rows []themeView
			if err := json.NewDecoder(response.Body).Decode(&rows); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range rows {
				if row.ID != id {
					continue
				}
				found = true
				if row.Name != authored.Name || row.Active != active || len(row.Warnings) != 0 || !reflect.DeepEqual(row.Tokens, authored.Tokens) {
					t.Fatalf("theme row = %+v, want active=%t and all authored tokens without warnings", row, active)
				}
				if row.Background != nil || row.Sky != nil || row.HasPreview {
					t.Fatalf("token-only example has image or sky: %+v", row)
				}
			}
			if found != present {
				t.Fatalf("theme present = %t, want %t", found, present)
			}
			if _, err := theme.Load(id); (err == nil) != present {
				t.Fatalf("theme load error = %v, want present=%t", err, present)
			}
		})
	}
	check("not-installed", false, false)
	preview := run(map[string]any{"source": source, "kind": "plugin", "apply": false})
	if preview.Status != "planned" || preview.Applied || preview.PlanID == "" || len(preview.Actions) != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	action := preview.Actions[0]
	if action.ThemeCount != 1 || action.SkillCount != 0 || action.AgentCount != 0 || action.CommandCount != 0 || action.PromptCount != 0 || action.HookCount != 0 || action.ToolCount != 0 || action.Runtime != nil {
		t.Fatalf("pure-theme preview = %+v", action)
	}
	check("preview", false, false)
	installed := run(map[string]any{"source": source, "kind": "plugin", "apply": true, "planId": preview.PlanID})
	if installed.Status != "done" || !installed.Applied || len(installed.Actions) != 1 {
		t.Fatalf("installed = %+v", installed)
	}
	installedRoot := installed.Actions[0].Target
	if installedRoot == "" {
		t.Fatal("install has no copied target")
	}
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	check("copied-source-removed", true, false)
	activate := func(themeID string, status int) {
		t.Helper()
		response := postJSON(t, srv.URL+"/themes", map[string]string{"id": themeID})
		defer response.Body.Close()
		if response.StatusCode != status {
			t.Fatalf("activate %q = %d, want %d", themeID, response.StatusCode, status)
		}
	}
	activate(id, http.StatusNoContent)
	check("active", true, true)
	if err := pluginpkg.SetEnabled(reasonixHome, "paper-dawn-kit", false); err != nil {
		t.Fatal(err)
	}
	check("disabled", false, false)
	activate(id, http.StatusUnprocessableEntity)
	if cfg, err := config.Load(); err != nil || cfg.Desktop.ThemePack != id {
		t.Fatalf("disabled selection was not retained: config=%+v err=%v", cfg, err)
	}
	if _, err := os.Stat(filepath.Join(installedRoot, "paper-dawn", "theme.json")); err != nil {
		t.Fatalf("disable removed copied content: %v", err)
	}
	if err := pluginpkg.SetEnabled(reasonixHome, "paper-dawn-kit", true); err != nil {
		t.Fatal(err)
	}
	check("re-enabled-active", true, true)
	removed := run(map[string]any{"op": "uninstall", "name": "paper-dawn-kit", "kind": "plugin"})
	if removed.Status != "done" || !removed.Applied {
		t.Fatalf("uninstall = %+v", removed)
	}
	check("removed", false, false)
	if _, err := os.Stat(installedRoot); !os.IsNotExist(err) {
		t.Fatalf("uninstall retained copied content: %v", err)
	}
	if cfg, err := config.Load(); err != nil || cfg.Desktop.ThemePack != id {
		t.Fatalf("removed selection was not retained: config=%+v err=%v", cfg, err)
	}
	activate("", http.StatusNoContent)
	if cfg, err := config.Load(); err != nil || cfg.Desktop.ThemePack != "" {
		t.Fatalf("default appearance was not restored: config=%+v err=%v", cfg, err)
	}
	check("default", false, false)
}
