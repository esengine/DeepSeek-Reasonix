package boot

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/plugin"
)

// v1.20+ removes product Safe Mode. REASONIX_SAFE_MODE no longer changes boot
// behavior; these tests pin that the normal tool/plugin surface stays available.

func TestBuildIgnoresSafeModeEnvForTools(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	t.Setenv("REASONIX_SAFE_MODE", "1")

	ctrl, err := Build(context.Background(), Options{
		SessionDir: filepath.Join(t.TempDir(), "sessions"),
		TokenMode:  TokenModeFull,
		Sink:       event.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range ctrl.AllToolContractEntries() {
		names[e.Name] = true
	}
	ctrl.Close()
	for _, name := range []string{"install_source", "run_skill", "read_skill", "use_capability"} {
		if !names[name] {
			t.Fatalf("REASONIX_SAFE_MODE must not strip %s from the capability registry", name)
		}
	}
}

func TestBuildMemoryMigrationFailureWarnsAndContinues(t *testing.T) {
	isolateConfigHome(t)
	project := robustTempDir(t)
	t.Setenv("REASONIX_SAFE_MODE", "")
	globalDir := filepath.Join(config.MemoryUserDir(), "memory", "global")
	if err := os.MkdirAll(filepath.Dir(globalDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	var notices []event.Event
	ctrl, err := Build(context.Background(), Options{
		WorkspaceRoot: project,
		SessionDir:    filepath.Join(t.TempDir(), "sessions"),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e)
			}
		}),
	})
	if err != nil {
		t.Fatalf("Build should continue after memory migration failure: %v", err)
	}
	ctrl.Close()
	for _, notice := range notices {
		if notice.Level == event.LevelWarn && strings.Contains(notice.Text, "Memory metadata migration") && notice.Detail != "" {
			return
		}
	}
	t.Fatalf("memory migration warning missing: %+v", notices)
}

func TestBuildNormalModeKeepsSourceConnectorAndSkillTools(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	t.Setenv("REASONIX_SAFE_MODE", "")

	for _, tokenMode := range []string{TokenModeFull, TokenModeDelivery} {
		ctrl, err := Build(context.Background(), Options{
			SessionDir: filepath.Join(t.TempDir(), "sessions"),
			TokenMode:  tokenMode,
			Sink:       event.Discard,
		})
		if err != nil {
			t.Fatalf("Build(%q): %v", tokenMode, err)
		}
		names := map[string]bool{}
		for _, e := range ctrl.AllToolContractEntries() {
			names[e.Name] = true
		}
		visible := map[string]bool{}
		for _, e := range ctrl.ToolContractEntries() {
			visible[e.Name] = true
		}
		ctrl.Close()
		// Full registry always has skill/install tools + use_capability.
		for _, name := range []string{"install_source", "run_skill", "read_skill", "use_capability"} {
			if !names[name] {
				t.Fatalf("normal mode (%q) must register %s in the capability registry", tokenMode, name)
			}
		}
		// Provider-visible surface is the unified core for every role setting.
		if !visible["use_capability"] {
			t.Fatalf("normal mode (%q) must expose use_capability", tokenMode)
		}
		if visible["connect_tool_source"] {
			t.Fatalf("normal mode (%q) must not expose connect_tool_source", tokenMode)
		}
	}
}

func TestBuildStillSpawnsExtraPluginsWhenSafeModeEnvSet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sentinel spec uses /bin/sh")
	}
	isolateConfigHome(t)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	t.Setenv("REASONIX_SAFE_MODE", "1")
	marker := filepath.Join(plugin.MCPStateDir(config.ReasonixHomeDir(), workspace, "acp-extra"), "started")
	ctrl, err := Build(context.Background(), Options{
		SessionDir: filepath.Join(t.TempDir(), "sessions"),
		Sink:       event.Discard,
		ExtraPlugins: []plugin.Spec{{
			Name:    "acp-extra",
			Command: "/bin/sh",
			Args:    []string{"-c", "echo started > '" + marker + "'"},
		}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ctrl.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("host-supplied MCP server never spawned; REASONIX_SAFE_MODE must not drop ExtraPlugins")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
