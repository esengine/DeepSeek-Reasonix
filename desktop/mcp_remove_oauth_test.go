package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/plugin"
)

func TestRemoveMCPServerReconcilesOAuthState(t *testing.T) {
	for _, keepFallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("keep-fallback-%v", keepFallback), func(t *testing.T) {
			isolateDesktopUserDirs(t)
			root, name, resource := robustTempDir(t), "shared", "https://mcp.example.test/mcp"
			t.Chdir(root)
			if keepFallback {
				cfg := config.LoadForEdit(config.UserConfigPath())
				cfg.Plugins = []config.PluginEntry{{Name: name, Type: "http", URL: resource}}
				if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
					t.Fatal(err)
				}
			}
			project := fmt.Sprintf("[[plugins]]\nname = %q\ntype = \"http\"\nurl = %q\n", name, resource)
			if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(project), 0o644); err != nil {
				t.Fatal(err)
			}
			stateDir := plugin.MCPStateDir(config.ReasonixHomeDir(), root, name)
			if err := os.MkdirAll(stateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(stateDir, "oauth.json")
			if err := os.WriteFile(statePath, fmt.Appendf(nil, `{"version":1,"resource":%q,"access_token":"private"}`, resource), 0o600); err != nil {
				t.Fatal(err)
			}

			app := NewApp()
			app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost(), WorkspaceRoot: root}), "")
			defer app.activeCtrl().Close()
			app.activeTab().WorkspaceRoot = root
			if err := app.RemoveMCPServer(name); err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(statePath)
			if keepFallback && statErr != nil || !keepFallback && !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("OAuth state after removal: keep=%v err=%v", keepFallback, statErr)
			}
		})
	}
}

func TestRemoveMCPServerReconcilesOAuthStateAcrossWorkspaceRuntimes(t *testing.T) {
	isolateDesktopUserDirs(t)
	activeRoot, otherRoot := robustTempDir(t), robustTempDir(t)
	name, resource := "shared", "https://mcp.example.test/mcp"
	t.Chdir(activeRoot)
	project := fmt.Sprintf("[[plugins]]\nname = %q\ntype = \"http\"\nurl = %q\n", name, resource)
	if err := os.WriteFile(filepath.Join(activeRoot, "reasonix.toml"), []byte(project), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{activeRoot, otherRoot} {
		stateDir := plugin.MCPStateDir(config.ReasonixHomeDir(), root, name)
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		statePath := filepath.Join(stateDir, "oauth.json")
		if err := os.WriteFile(statePath, fmt.Appendf(nil, `{"version":1,"resource":%q,"access_token":"private"}`, resource), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	app := NewApp()
	active := control.New(control.Options{Host: plugin.NewHost(), WorkspaceRoot: activeRoot})
	app.setTestCtrl(active, "")
	defer active.Close()
	app.activeTab().WorkspaceRoot = activeRoot
	app.tabs["other"] = &WorkspaceTab{ID: "other", WorkspaceRoot: otherRoot}
	if err := app.RemoveMCPServer(name); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{activeRoot, otherRoot} {
		statePath := filepath.Join(plugin.MCPStateDir(config.ReasonixHomeDir(), root, name), "oauth.json")
		if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("OAuth state for workspace %q still exists: %v", root, err)
		}
	}
}
