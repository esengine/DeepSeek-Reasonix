package cli

import (
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/plugin"
)

func TestMCPAddFallbackNameRefusesCollisionWithoutOverwriting(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	workspace := testenv.TempDir(t)
	t.Chdir(workspace)
	var probed []config.PluginEntry
	previous := mcpProbeForInstall
	mcpProbeForInstall = func(entry config.PluginEntry) (plugin.MCPInstallResult, error) {
		probed = append(probed, entry)
		return plugin.ReadyInstallResult(entry.Name, 1), nil
	}
	t.Cleanup(func() { mcpProbeForInstall = previous })
	first := []string{"run", "--unknown", "first-option", "first-image"}
	second := []string{"run", "--unknown=second-option", "second-image"}
	if code := mcpAddCLI(append([]string{"--", "docker"}, first...)); code != 0 {
		t.Fatalf("first install exited %d", code)
	}
	before, err := config.LoadForRoot(workspace)
	if err != nil || len(before.Plugins) != 1 || len(probed) != 1 {
		t.Fatalf("first config=%+v probes=%v err=%v", before, probed, err)
	}
	if entry := before.Plugins[0]; entry.Name != "mcp-server" || entry.Command != "docker" || !reflect.DeepEqual(entry.Args, first) {
		t.Fatalf("first entry = %+v", entry)
	}
	if code := mcpAddCLI(append([]string{"--", "docker"}, second...)); code == 0 || len(probed) != 1 {
		t.Fatalf("duplicate exit=%d probes=%d; want rejection before probing", code, len(probed))
	}
	after, err := config.LoadForRoot(workspace)
	if err != nil || !reflect.DeepEqual(after.Plugins, before.Plugins) {
		t.Fatalf("duplicate replaced config: before=%+v after=%+v err=%v", before, after, err)
	}
	if code := mcpAddCLI(append([]string{"second-server", "--", "docker"}, second...)); code != 0 {
		t.Fatalf("explicit-name install exited %d", code)
	}
	after, err = config.LoadForRoot(workspace)
	if err != nil || len(after.Plugins) != 2 || len(probed) != 2 {
		t.Fatalf("explicit-name config=%+v probes=%v err=%v", after, probed, err)
	}
	if entry := probed[1]; entry.Name != "second-server" || entry.Command != "docker" || !reflect.DeepEqual(entry.Args, second) {
		t.Errorf("explicit-name probe = %+v", entry)
	}
	if entry := after.Plugins[0]; !reflect.DeepEqual(entry, before.Plugins[0]) {
		t.Errorf("explicit-name install changed first entry: %+v", entry)
	}
	if entry := after.Plugins[1]; entry.Name != "second-server" || entry.Command != "docker" || !reflect.DeepEqual(entry.Args, second) {
		t.Errorf("explicit-name entry = %+v", entry)
	}
}
