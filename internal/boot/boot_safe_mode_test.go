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

func TestBuildSafeModeLeavesDeprecatedStepLimitsUntouched(t *testing.T) {
	isolateConfigHome(t)
	project := robustTempDir(t)
	t.Setenv("REASONIX_SAFE_MODE", "1")
	raw := []byte(`default_model = "broken-model"

[agent]
max_steps = 2
planner_max_steps = 3

[[providers]]
name = "broken-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
`)
	path := filepath.Join(project, "reasonix.toml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
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
		t.Fatalf("Build: %v", err)
	}
	ctrl.Close()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("Safe Mode rewrote project config:\n--- got ---\n%s\n--- want ---\n%s", got, raw)
	}
	for _, notice := range notices {
		if strings.Contains(notice.Text, "Deprecated agent step") {
			t.Fatalf("Safe Mode reported a migration that it must not run: %+v", notice)
		}
	}
}

// TestBuildNormalModeKeepsSourceConnectorAndSkillTools is the inverse of
// TestBuildSafeModeOmitsSourceConnectorAndSkillTools: it pins the normal-mode
// tool surface so an inverted or over-broad Safe Mode gate that stripped
// install_source, the skill tools, or the Economy connector from ordinary
// sessions fails directly instead of relying on incidental coverage.
func TestBuildNormalModeKeepsSourceConnectorAndSkillTools(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	t.Setenv("REASONIX_SAFE_MODE", "")

	for _, tokenMode := range []string{TokenModeFull, TokenModeEconomy} {
		ctrl, err := Build(context.Background(), Options{
			SessionDir: filepath.Join(t.TempDir(), "sessions"),
			TokenMode:  tokenMode,
			Sink:       event.Discard,
		})
		if err != nil {
			t.Fatalf("Build(%q): %v", tokenMode, err)
		}
		names := map[string]bool{}
		for _, e := range ctrl.ToolContractEntries() {
			names[e.Name] = true
		}
		ctrl.Close()
		// Economy registers slash_command lazily through the connector, so only
		// the connector itself is part of its boot surface.
		want := []string{"connect_tool_source"}
		if tokenMode == TokenModeFull {
			want = []string{"install_source", "run_skill", "read_skill", "read_only_skill", "slash_command"}
		}
		for _, name := range want {
			if !names[name] {
				t.Fatalf("normal mode (%q) must register %s", tokenMode, name)
			}
		}
	}
}

// TestBuildSafeModeDropsExtraPlugins proves a recovery boot never starts
// host-supplied MCP servers (e.g. ACP session servers): the sentinel command
// writes a marker inside its sandbox-approved private state directory when
// spawned, and Safe Mode must never run it. The
// normal-mode half proves the fixture actually exercises the spawn path, so
// the safe-mode half cannot pass vacuously.
func TestBuildSafeModeDropsExtraPlugins(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sentinel spec uses /bin/sh")
	}
	build := func(t *testing.T, marker string) {
		t.Helper()
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
	}

	t.Run("safe mode never spawns the server", func(t *testing.T) {
		isolateConfigHome(t)
		workspace := robustTempDir(t)
		t.Chdir(workspace)
		t.Setenv("REASONIX_SAFE_MODE", "1")
		marker := filepath.Join(plugin.MCPStateDir(config.ReasonixHomeDir(), workspace, "acp-extra"), "started")
		build(t, marker)
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("safe mode spawned the host-supplied MCP server (stat err=%v)", err)
		}
	})

	t.Run("normal mode exercises the spawn path", func(t *testing.T) {
		isolateConfigHome(t)
		workspace := robustTempDir(t)
		t.Chdir(workspace)
		t.Setenv("REASONIX_SAFE_MODE", "")
		marker := filepath.Join(plugin.MCPStateDir(config.ReasonixHomeDir(), workspace, "acp-extra"), "started")
		build(t, marker)
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(marker); err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("host-supplied MCP server never spawned in normal mode; the fixture no longer exercises the gate")
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
}
