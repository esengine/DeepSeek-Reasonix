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

	"reasonix/internal/contract/event"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
)

func TestEffectPluginUpdateReplacesHTTPMCPContributions(t *testing.T) {
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	oldCall := filepath.Join(workspace, "old-server-call")
	newCall := filepath.Join(workspace, "new-server-call")
	oldServer := deployMCPServer(t, oldCall)
	defer oldServer.Close()
	newServer := deployMCPServer(t, newCall)
	defer newServer.Close()

	revision := func(version, name, url string) string {
		t.Helper()
		source := robustTempDir(t)
		writeFile(t, source, pluginpkg.NativeManifest, fmt.Sprintf(`{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "ecosystem-update",
  "version": %q,
  "contributes": {
    "mcpServers": {
      %q: {"type": "http", "url": %q, "load": "always"}
    }
  }
}`, version, name, url))
		return source
	}
	installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
	install := func(source, version string, replace bool) {
		t.Helper()
		request := map[string]any{"source": source, "kind": "plugin", "mode": "copy", "replace": replace}
		type result struct {
			OK      bool   `json:"ok"`
			Status  string `json:"status"`
			Applied bool   `json:"applied"`
			PlanID  string `json:"planId"`
		}
		run := func() result {
			t.Helper()
			args, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			out, err := installer.Execute(t.Context(), args)
			if err != nil {
				t.Fatalf("install_source: %v", err)
			}
			var response result
			if err := json.Unmarshal([]byte(out), &response); err != nil {
				t.Fatalf("install_source response %q: %v", out, err)
			}
			if !response.OK {
				t.Fatalf("install_source: %s", out)
			}
			return response
		}
		preview := run()
		if preview.Status != "planned" || preview.Applied || preview.PlanID == "" {
			t.Fatalf("preview = %+v", preview)
		}
		current, found, err := pluginpkg.FindInstalled(reasonixHome, "ecosystem-update")
		if err != nil || found != replace || (replace && current.Version != "1.0.0") {
			t.Fatalf("preview changed existing installation: %+v, found=%t, err=%v", current, found, err)
		}
		request["apply"], request["planId"] = true, preview.PlanID
		applied := run()
		if applied.Status != "done" || !applied.Applied {
			t.Fatalf("applied = %+v", applied)
		}
		current, found, err = pluginpkg.FindInstalled(reasonixHome, "ecosystem-update")
		if err != nil || !found || current.Version != version || !current.Enabled {
			t.Fatalf("installed version = %+v, found=%t, err=%v", current, found, err)
		}
	}
	callInstalled := func(name, absent string) {
		t.Helper()
		ctrl, err := Build(t.Context(), Options{Sink: event.Discard})
		if err != nil {
			t.Fatalf("Build after installation: %v", err)
		}
		defer ctrl.Close()
		waitForMCPServer(t, ctrl.Host(), name)
		if absent != "" {
			if _, err := ctrl.Host().ToolsFor(t.Context(), absent); err == nil {
				t.Fatalf("updated package still exposes %q", absent)
			}
		}
		tools, err := ctrl.Host().ToolsFor(t.Context(), name)
		if err != nil || len(tools) != 1 || tools[0].Name() != "mcp__"+name+"__deploy" {
			t.Fatalf("installed tools = %v, err=%v", tools, err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		result, err := tools[0].Execute(ctx, json.RawMessage(`{}`))
		if err != nil || !strings.Contains(result, "deployed") {
			t.Fatalf("installed tool result = %q, err=%v", result, err)
		}
	}

	install(revision("1.0.0", "oldops", oldServer.URL), "1.0.0", false)
	callInstalled("oldops", "")
	if err := os.Remove(oldCall); err != nil {
		t.Fatalf("initial tool did not call the old server: %v", err)
	}
	install(revision("2.0.0", "newops", newServer.URL), "2.0.0", true)
	callInstalled("newops", "oldops")
	if data, err := os.ReadFile(newCall); err != nil || string(data) != "deployed\n" {
		t.Fatalf("updated tool did not call the new server: %q, err=%v", data, err)
	}
	if _, err := os.Stat(oldCall); !os.IsNotExist(err) {
		t.Fatalf("updated tool called the old server: %v", err)
	}
}
