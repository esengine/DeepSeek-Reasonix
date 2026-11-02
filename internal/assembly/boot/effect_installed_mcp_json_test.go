package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/ext/installsource"
)

func TestEffectInstalledStandaloneMCPJSONToolCanBeCalled(t *testing.T) {
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	server := deployMCPServer(t, "")
	defer server.Close()

	source := filepath.Join(robustTempDir(t), ".mcp.json")
	writeFile(t, filepath.Dir(source), filepath.Base(source), fmt.Sprintf(`{"mcpServers":{"ops":{"type":"http","url":%q,"load":"always"}}}`, server.URL))
	installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home})
	args, err := json.Marshal(map[string]any{"source": source, "kind": "mcp", "scope": "global", "apply": true})
	if err != nil {
		t.Fatal(err)
	}
	out, err := installer.Execute(t.Context(), args)
	if err != nil {
		t.Fatalf("install_source: %v", err)
	}
	var applied struct {
		OK      bool   `json:"ok"`
		Status  string `json:"status"`
		Applied bool   `json:"applied"`
		Actions []struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"actions"`
	}
	if err := json.Unmarshal([]byte(out), &applied); err != nil {
		t.Fatalf("install_source response %q: %v", out, err)
	}
	if !applied.OK || applied.Status != "done" || !applied.Applied || len(applied.Actions) != 1 || applied.Actions[0].Kind != "mcp" || applied.Actions[0].Status != "done" {
		t.Fatalf("installation = %s", out)
	}

	ctrl, err := Build(t.Context(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build installed MCP: %v", err)
	}
	defer ctrl.Close()
	waitForMCPServer(t, ctrl.Host(), "ops")
	tools, err := ctrl.Host().ToolsFor(t.Context(), "ops")
	if err != nil {
		t.Fatalf("ToolsFor installed MCP: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__ops__deploy" {
		t.Fatalf("installed MCP tools = %v", tools)
	}
	callCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	result, err := tools[0].Execute(callCtx, json.RawMessage(`{}`))
	cancel()
	if err != nil || !strings.Contains(result, "deployed") {
		t.Fatalf("installed MCP tool result = %q, err=%v", result, err)
	}
	if _, err := ctrl.RemoveMCPServer("ops"); err != nil {
		t.Fatalf("remove installed MCP: %v", err)
	}
	if _, err := ctrl.Host().ToolsFor(t.Context(), "ops"); err == nil {
		t.Fatal("removed MCP server still exposes its tool")
	}
	ctrl.Close()

	restarted, err := Build(t.Context(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build after removal: %v", err)
	}
	defer restarted.Close()
	if _, err := restarted.Host().ToolsFor(t.Context(), "ops"); err == nil {
		t.Fatal("removed MCP server returned after restart")
	}
}

func TestEffectRemoveMCPAfterSourceDirectoryDeleted(t *testing.T) {
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	server := deployMCPServer(t, "")
	defer server.Close()
	sourceDir := filepath.Join(workspace, ".reasonix")
	writeFile(t, sourceDir, "reasonix.toml", fmt.Sprintf("[[plugins]]\nname = \"fssc-oracle\"\ntype = \"http\"\nurl = %q\nload = \"always\"\n", server.URL))
	cfg, err := config.LoadForRoot(workspace)
	if err != nil || len(cfg.Plugins) != 1 {
		t.Fatalf("load project MCP: %v, %v", cfg, err)
	}
	if err := config.DefaultActivationStore().SetServerEnabled(cfg.Plugins[0], workspace, config.ActivationProject, true); err != nil {
		t.Fatal(err)
	}
	ctrl, err := Build(t.Context(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	waitForMCPServer(t, ctrl.Host(), "fssc-oracle")
	tools, err := ctrl.Host().ToolsFor(t.Context(), "fssc-oracle")
	if err != nil || len(tools) != 1 {
		t.Fatalf("live tools before source deletion: %v, %v", tools, err)
	}
	if result, err := tools[0].Execute(t.Context(), json.RawMessage(`{}`)); err != nil || !strings.Contains(result, "deployed") {
		t.Fatalf("live MCP call = %q, %v", result, err)
	}
	if err := os.RemoveAll(sourceDir); err != nil {
		t.Fatal(err)
	}
	if configured := ctrl.ConfiguredMCPServers(); len(configured) != 0 {
		t.Fatalf("deleted source is still configured: %+v", configured)
	}
	if health := ctrl.MCPServerHealth(); len(health) != 1 || health[0].Name != "fssc-oracle" {
		t.Fatalf("live stale row before removal: %+v", health)
	}
	if disconnected, err := ctrl.RemoveMCPServer("fssc-oracle"); err != nil || !disconnected {
		t.Fatalf("remove stale live MCP = (%v, %v)", disconnected, err)
	}
	if _, err := ctrl.Host().ToolsFor(t.Context(), "fssc-oracle"); err == nil {
		t.Fatal("removed stale MCP still exposes tools")
	}
	if health := ctrl.MCPServerHealth(); len(health) != 0 {
		t.Fatalf("removed stale MCP still has a status row: %+v", health)
	}
	ctrl.Close()
	restarted, err := Build(t.Context(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if health := restarted.MCPServerHealth(); len(health) != 0 {
		t.Fatalf("stale MCP returned after restart: %+v", health)
	}
}
