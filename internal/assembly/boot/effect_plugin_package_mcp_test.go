package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/ext/pluginpkg"
)

func TestEffectInstalledPluginMCPToolCanBeCalledAndDisabled(t *testing.T) {
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)

	root := pluginpkg.InstallRoot(reasonixHome, "ecosystem")
	manifest := fmt.Sprintf(`{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "ecosystem",
  "version": "1.0.0",
  "contributes": {
    "mcpServers": {
      "helper": {
        "type": "stdio",
        "command": %q,
        "args": ["-test.run=TestHelperProcess", "--"],
        "env": {"GO_WANT_HELPER_PROCESS": "1"},
        "load": "always"
      }
    }
  }
}`, os.Args[0])
	writeFile(t, root, pluginpkg.NativeManifest, manifest)
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{
		Name: "ecosystem", Root: pluginpkg.RelativeRoot(reasonixHome, root), Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	ctrl, err := Build(t.Context(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build enabled package: %v", err)
	}
	waitForMCPServer(t, ctrl.Host(), "helper")
	tools, err := ctrl.Host().ToolsFor(t.Context(), "helper")
	if err != nil {
		ctrl.Close()
		t.Fatalf("ToolsFor installed package: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__helper__echo" {
		ctrl.Close()
		t.Fatalf("installed package tools = %v", tools)
	}
	callCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	result, err := tools[0].Execute(callCtx, json.RawMessage(`{"msg":"from-package"}`))
	cancel()
	ctrl.Close()
	if err != nil || result != "echo: from-package" {
		t.Fatalf("package tool result = %q, err=%v", result, err)
	}

	if err := pluginpkg.SetEnabled(reasonixHome, "ecosystem", false); err != nil {
		t.Fatal(err)
	}
	disabled, err := Build(t.Context(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build disabled package: %v", err)
	}
	defer disabled.Close()
	if _, err := disabled.Host().ToolsFor(t.Context(), "helper"); err == nil {
		t.Fatal("disabled package still exposes its MCP server")
	}
}
