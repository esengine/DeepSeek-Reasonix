package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func bindingHome(t *testing.T) (*ActivationStore, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("BINDING_FLAG", "")
	_ = os.Unsetenv("BINDING_FLAG")
	return DefaultActivationStore(), t.TempDir()
}

func writeProjectFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func declaredServer(t *testing.T, root, name string) PluginEntry {
	t.Helper()
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Plugins {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("%s not declared in %s", name, root)
	return PluginEntry{}
}

func mcpJSON(t *testing.T, server map[string]any) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"srv": server}})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// An enable must cover the file the launcher actually starts. A bare command
// is looked up in the declaration's own PATH; one that points into the
// workspace runs a repository file, and changing it must ask again.
func TestEnableCoversTheFileADeclaredPathRuns(t *testing.T) {
	store, root := bindingHome(t)
	writeProjectFiles(t, root, map[string]string{
		".mcp.json": mcpJSON(t, map[string]any{"command": "rxtool",
			"env": map[string]string{"PATH": "${CLAUDE_PROJECT_DIR}" + string(filepath.Separator) + "tools" + string(filepath.ListSeparator) + "/usr/bin"}}),
		"tools/rxtool": "#!/bin/sh\necho approved\n",
	})
	if err := store.SetServerEnabled(declaredServer(t, root, "srv"), root, ActivationProject, true); err != nil {
		t.Fatal(err)
	}
	writeProjectFiles(t, root, map[string]string{"tools/rxtool": "#!/bin/sh\necho swapped\n"})
	if on, _ := store.IsEnabled(declaredServer(t, root, "srv"), root); on {
		t.Fatal("the executable a declared PATH reaches changed and the enable still holds")
	}
}

// On Windows `./scripts/mcp` runs scripts/mcp.cmd when both exist, so the
// enable must cover the sibling the platform picks.
func TestEnableCoversThePathextSiblingWindowsRuns(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PATHEXT probing is the platform's own here; TestEnableCoversPathextSiblingsOnEveryOS fakes it")
	}
	assertPathextSiblingBound(t)
}

func assertPathextSiblingBound(t *testing.T) {
	t.Helper()
	store, root := bindingHome(t)
	writeProjectFiles(t, root, map[string]string{
		".mcp.json":       mcpJSON(t, map[string]any{"command": "./scripts/mcp"}),
		"scripts/mcp":     "#!/bin/sh\n",
		"scripts/mcp.cmd": "@echo approved\r\n",
	})
	if err := store.SetServerEnabled(declaredServer(t, root, "srv"), root, ActivationProject, true); err != nil {
		t.Fatal(err)
	}
	writeProjectFiles(t, root, map[string]string{"scripts/mcp.cmd": "@echo swapped\r\n"})
	if on, _ := store.IsEnabled(declaredServer(t, root, "srv"), root); on {
		t.Fatal("scripts/mcp.cmd changed and the enable still holds")
	}
}
