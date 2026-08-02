package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/repair"
)

func TestConfigRepairStatusClearsDamagedStateAfterManualFix(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	if got := app.ConfigRepairStatus(); got.Outcome != "config_damaged" {
		t.Fatalf("damaged status = %+v", got)
	}
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := app.ConfigRepairStatus(); got.Outcome == "config_damaged" {
		t.Fatalf("manually repaired config stayed damaged: %+v", got)
	}
	app.mu.RLock()
	damaged := app.globalConfigDamaged
	app.mu.RUnlock()
	if damaged {
		t.Fatal("globalConfigDamaged stayed latched after a valid reload")
	}
}

func TestUndoConfigRepairRejectsStaleBannerTransaction(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	global := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(global), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte("command = \"D:\\开发\\tool.exe\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := repair.ApplyConfigEscapes(repair.ConfigEscapesOptions{}); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	banner := app.ConfigRepairStatus()
	if !banner.Undoable || banner.TransactionID == "" {
		t.Fatalf("repair banner lacks transaction token: %+v", banner)
	}

	root := t.TempDir()
	project := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(project, []byte("[[plugins]]\ncommand = \"C:\\dev\\bridge.exe\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	preview, err := repair.InspectConfigEscapes(repair.ConfigEscapesOptions{Root: root, IncludeProject: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repair.ApplyConfigEscapes(repair.ConfigEscapesOptions{
		Root:           root,
		IncludeProject: true,
		ExpectedStates: map[string]string{project: preview.Project.StateID},
	}); err != nil {
		t.Fatal(err)
	}
	newer, err := repair.ReadLastRepair()
	if err != nil {
		t.Fatal(err)
	}
	repairedProject, err := os.ReadFile(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.UndoConfigRepair(banner.TransactionID); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("stale banner undo err = %v, want expired action", err)
	}
	current, err := repair.ReadLastRepair()
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != newer.ID || current.Undone {
		t.Fatalf("newer repair changed by stale banner: %+v", current)
	}
	if got, err := os.ReadFile(project); err != nil || string(got) != string(repairedProject) {
		t.Fatalf("stale banner modified project config: got=%q err=%v", got, err)
	}
}
