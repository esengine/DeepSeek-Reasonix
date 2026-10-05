package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
)

func TestEffectInstalledPluginHTTPMCPToolCanBeCalledAndDisabled(t *testing.T) {
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	server := deployMCPServer(t, "")
	defer server.Close()

	source := robustTempDir(t)
	writeFile(t, source, pluginpkg.NativeManifest, fmt.Sprintf(`{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "ecosystem-http",
  "version": "1.0.0",
  "contributes": {
    "mcpServers": {
      "ops": {"type": "http", "url": %q, "load": "always"}
    }
  }
}`, server.URL))
	installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home})
	args, err := json.Marshal(map[string]any{"source": source, "kind": "plugin", "apply": true})
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
	if !applied.OK || applied.Status != "done" || !applied.Applied || len(applied.Actions) != 1 || applied.Actions[0].Kind != "plugin" || applied.Actions[0].Status != "done" {
		t.Fatalf("installation = %s", out)
	}

	ctrl, err := Build(t.Context(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build enabled package: %v", err)
	}
	waitForMCPServer(t, ctrl.Host(), "ops")
	tools, err := ctrl.Host().ToolsFor(t.Context(), "ops")
	if err != nil {
		ctrl.Close()
		t.Fatalf("ToolsFor installed HTTP server: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__ops__deploy" {
		ctrl.Close()
		t.Fatalf("installed HTTP tools = %v", tools)
	}
	callCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	result, err := tools[0].Execute(callCtx, json.RawMessage(`{}`))
	cancel()
	ctrl.Close()
	if err != nil || !strings.Contains(result, "deployed") {
		t.Fatalf("installed HTTP tool result = %q, err=%v", result, err)
	}

	if err := pluginpkg.SetEnabled(reasonixHome, "ecosystem-http", false); err != nil {
		t.Fatal(err)
	}
	disabled, err := Build(t.Context(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build disabled package: %v", err)
	}
	defer disabled.Close()
	if _, err := disabled.Host().ToolsFor(context.Background(), "ops"); err == nil {
		t.Fatal("disabled package still exposes its HTTP MCP server")
	}
}
