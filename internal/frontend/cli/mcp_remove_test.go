package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/plugin"
)

func TestMCPRemoveCLIClearsReasonixOAuthState(t *testing.T) {
	isolateCLIConfigHome(t)
	workspace := testenv.TempDir(t)
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
