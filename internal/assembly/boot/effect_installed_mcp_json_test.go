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
