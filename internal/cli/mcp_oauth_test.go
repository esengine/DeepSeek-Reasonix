package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/plugin"
)

func TestMCPAuthCLIUsesReasonixPrivateState(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Chdir(workspace)
	entry := config.PluginEntry{Name: "figma", Type: "http", URL: "https://mcp.figma.com/mcp", Source: config.MCPSourceUserConfig}
	if _, err := config.InstallUserPluginForRoot(workspace, entry, true); err != nil {
		t.Fatal(err)
	}

	previous := mcpAuthorizeForCLI
	mcpAuthorizeForCLI = func(_ context.Context, spec plugin.Spec, openURL func(string) error) error {
		if spec.Name != "figma" || spec.URL != entry.URL {
			t.Fatalf("authorization spec = %+v", spec)
		}
		if spec.StateDir == "" || strings.HasPrefix(filepath.Clean(spec.StateDir), filepath.Clean(workspace)+string(filepath.Separator)) {
			t.Fatalf("OAuth state dir must be private Reasonix state, got %q", spec.StateDir)
		}
		if spec.OAuthHTTPClient == nil || openURL == nil {
			t.Fatal("authorization requires the proxy-aware client and browser opener")
		}
		return nil
	}
	t.Cleanup(func() { mcpAuthorizeForCLI = previous })
	if code := mcpAuthCLI([]string{"figma"}); code != 0 {
		t.Fatalf("mcp auth exit = %d", code)
	}
}

func TestMCPAuthCLIRejectsIneligibleConfigurations(t *testing.T) {
	tests := []struct {
		name  string
		entry config.PluginEntry
	}{
		{name: "stdio", entry: config.PluginEntry{Name: "server", Type: "stdio", Command: "server-mcp"}},
		{name: "legacy SSE", entry: config.PluginEntry{Name: "server", Type: "sse", URL: "https://mcp.example.test/sse"}},
		{name: "static authentication", entry: config.PluginEntry{
			Name: "server", Type: "http", URL: "https://mcp.example.test/mcp",
			Headers: map[string]string{"Authorization": "Bearer configured"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateCLIConfigHome(t)
			workspace := t.TempDir()
			t.Chdir(workspace)
			tc.entry.Source = config.MCPSourceUserConfig
			if _, err := config.InstallUserPluginForRoot(workspace, tc.entry, true); err != nil {
				t.Fatal(err)
			}

			called := false
			previous := mcpAuthorizeForCLI
			mcpAuthorizeForCLI = func(context.Context, plugin.Spec, func(string) error) error {
				called = true
				return nil
			}
			t.Cleanup(func() { mcpAuthorizeForCLI = previous })
			if code := mcpAuthCLI([]string{"server"}); code == 0 {
				t.Fatal("mcp auth unexpectedly accepted an ineligible server")
			}
			if called {
				t.Fatal("ineligible server reached the OAuth implementation")
			}
		})
	}
}
