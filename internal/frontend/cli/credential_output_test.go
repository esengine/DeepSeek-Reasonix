package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func TestMCPCommandsDoNotPrintOperationalCredentials(t *testing.T) {
	isolateCLIConfigHome(t)
	workspace := testenv.TempDir(t)
	t.Chdir(workspace)
	entries := []config.PluginEntry{
		{Name: "neutral-http", Type: "http", URL: "https://host/mcp?%74oken=fixture-secret#passwd=fixture-secret", Headers: map[string]string{"Authorization": "fixture-secret"}},
		{Name: "neutral-stdio", Command: "node", Args: []string{"--password", "fixture-secret"}, Env: map[string]string{"PASSWORD": "fixture-secret", "author": "neutral"}},
	}
	servers := map[string]config.PluginEntry{}
	for _, entry := range entries {
		servers[entry.Name] = entry
	}
	raw, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".mcp.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = previous; reader.Close(); writer.Close() })
	if mcpList() != 0 {
		t.Fatal("list failed")
	}
	for _, entry := range entries {
		if mcpGetCLI([]string{entry.Name}) != 0 {
			t.Fatal("get failed")
		}
	}
	writer.Close()
	os.Stdout = previous
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "fixture-secret") {
		t.Fatalf("CLI leaked: %s", out)
	}
	if !strings.Contains(string(out), "author=neutral") {
		t.Fatalf("ordinary env hidden: %s", out)
	}
}
