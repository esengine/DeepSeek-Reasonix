package cli

import (
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/plugin"
)

func TestMCPAddPNPMDlxPersistsDistinctServerNames(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_STATE_HOME", testenv.TempDir(t))
	workspace := testenv.TempDir(t)
	t.Chdir(workspace)
	var probed []config.PluginEntry
	previous := mcpProbeForInstall
	mcpProbeForInstall = func(entry config.PluginEntry) (plugin.MCPInstallResult, error) {
		probed = append(probed, entry)
		return plugin.ReadyInstallResult(entry.Name, 1), nil
	}
	t.Cleanup(func() { mcpProbeForInstall = previous })
	entries := []struct {
		name string
		args []string
	}{
		{"docs-mcp", []string{"dlx", "@example/docs-mcp@1.2.0", "--port", "3456"}},
		{"search-mcp", []string{"--package", "@example/search-client", "dlx", "search-mcp"}},
	}
	for _, entry := range entries {
		if code := mcpAddCLI(append([]string{"--", "pnpm"}, entry.args...)); code != 0 {
			t.Fatalf("adding %s exited %d", entry.name, code)
		}
	}
	cfg, err := config.LoadForRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins) != len(entries) || len(probed) != len(entries) {
		t.Fatalf("persisted %d entries, probed %d; want two distinct entries", len(cfg.Plugins), len(probed))
	}
	for i, want := range entries {
		if probe := probed[i]; probe.Name != want.name || probe.Command != "pnpm" || !reflect.DeepEqual(probe.Args, want.args) {
			t.Errorf("probe = %+v, want %s with unchanged argv", probe, want.name)
		}
		found := false
		for _, got := range cfg.Plugins {
			if got.Name == want.name {
				found = true
				if got.Command != "pnpm" || !reflect.DeepEqual(got.Args, want.args) || got.Source != config.MCPSourceUserConfig {
					t.Errorf("persisted entry = %+v", got)
				}
			}
		}
		if !found {
			t.Errorf("missing persisted %s", want.name)
		}
	}
	if code := mcpAddCLI(append([]string{"--", "pnpm"}, entries[0].args...)); code == 0 || len(probed) != 2 {
		t.Errorf("duplicate exit=%d, probes=%d; want rejection before probing", code, len(probed))
	}
}
