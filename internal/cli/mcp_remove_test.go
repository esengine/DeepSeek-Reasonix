package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/plugin"
)

func TestMCPRemoveCLIClearsReasonixOAuthState(t *testing.T) {
	isolateCLIConfigHome(t)
	workspace := t.TempDir()
	t.Chdir(workspace)
	entry := config.PluginEntry{
		Name: "figma", Type: "http", URL: "https://mcp.figma.com/mcp", Source: config.MCPSourceUserConfig,
	}
	if _, err := config.InstallUserPluginForRoot(workspace, entry, true); err != nil {
		t.Fatal(err)
	}
	oauthState := writeCLIAuthState(t, workspace, entry.Name, entry.URL)

	if code := mcpRemoveCLI([]string{entry.Name}); code != 0 {
		t.Fatalf("mcp remove exit = %d", code)
	}
	if _, err := os.Stat(oauthState); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OAuth state still exists after CLI removal: %v", err)
	}
}

func TestMCPRemoveCLIPreservesOAuthStateForMatchingFallback(t *testing.T) {
	isolateCLIConfigHome(t)
	workspace := t.TempDir()
	t.Chdir(workspace)
	const resource = "https://mcp.example.test/mcp"
	global := config.PluginEntry{
		Name: "shared", Type: "http", URL: resource, Source: config.MCPSourceUserConfig,
	}
	if _, err := config.InstallUserPluginForRoot(workspace, global, true); err != nil {
		t.Fatal(err)
	}
	projectConfig := minimalTestModelTOML + `
[[plugins]]
name = "shared"
type = "http"
url = "https://mcp.example.test/mcp"
`
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	oauthState := writeCLIAuthState(t, workspace, global.Name, resource)

	if code := mcpRemoveCLI([]string{global.Name}); code != 0 {
		t.Fatalf("mcp remove exit = %d", code)
	}
	if _, err := os.Stat(oauthState); err != nil {
		t.Fatalf("matching fallback OAuth state was removed: %v", err)
	}
}

func writeCLIAuthState(t *testing.T, workspace, name, resource string) string {
	t.Helper()
	stateDir := plugin.MCPStateDir(config.ReasonixHomeDir(), workspace, name)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "oauth.json")
	raw := []byte(`{"version":1,"resource":"` + resource + `","access_token":"private"}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
