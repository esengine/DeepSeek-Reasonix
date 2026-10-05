package boot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
)

func TestEffectCompatiblePluginMCPInstallLifecycle(t *testing.T) {
	for _, tc := range []struct {
		kind     string
		manifest string
	}{
		{kind: "codex", manifest: pluginpkg.CodexManifest},
		{kind: "claude", manifest: pluginpkg.ClaudeManifest},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			home := isolateConfigHome(t)
			reasonixHome := filepath.Join(home, ".reasonix")
			t.Setenv("REASONIX_HOME", reasonixHome)
			workspace := robustTempDir(t)
			t.Chdir(workspace)
			var recorder *effectRecordingProvider
			providerKind := "boot-compatible-mcp-" + tc.kind
			provider.Register(providerKind, func(provider.Config) (provider.Provider, error) { return recorder, nil })
			writeFile(t, workspace, "reasonix.toml", fmt.Sprintf(`
default_model = "test-model"
[agent]
system_prompt = "BASE"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = %q
model = "x"
`, providerKind))
			approveWorkspace(t, workspace)
			server := deployMCPServer(t, "")
			t.Cleanup(server.Close)
			source := robustTempDir(t)
			writeFile(t, source, tc.manifest, `{"name":"compatible-ops","version":"1.0.0"}`)
			writeFile(t, source, ".mcp.json", fmt.Sprintf(`{"mcpServers":{"ops":{"type":"streamable-http","url":%q}}}`, server.URL))

			installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
			request := map[string]any{"source": source, "kind": "plugin", "mode": "copy"}
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
				if err := json.Unmarshal([]byte(out), &response); err != nil || !response.OK {
					t.Fatalf("install_source = %s, err=%v", out, err)
				}
				return response
			}
			preview := run()
			if preview.Applied || preview.Status != "planned" || preview.PlanID == "" {
				t.Fatalf("preview = %+v", preview)
			}
			if installed, warnings := pluginpkg.LoadInstalled(reasonixHome); len(installed) != 0 || len(warnings) != 0 {
				t.Fatalf("preview changed installed packages: %+v, warnings=%v", installed, warnings)
			}
			request["apply"], request["planId"] = true, preview.PlanID
			if applied := run(); !applied.Applied || applied.Status != "done" {
				t.Fatalf("applied = %+v", applied)
			}
			if installed, warnings := pluginpkg.LoadInstalled(reasonixHome); len(installed) != 1 || len(warnings) != 0 || installed[0].Package.ManifestKind != tc.kind {
				t.Fatalf("installed packages = %+v, warnings=%v", installed, warnings)
			}
			if err := os.RemoveAll(source); err != nil {
				t.Fatal(err)
			}

			check := func(present, activated bool) {
				t.Helper()
				recorder = &effectRecordingProvider{}
				ctrl, err := Build(t.Context(), Options{Sink: event.Discard})
				if err != nil {
					t.Fatalf("Build: %v", err)
				}
				defer ctrl.Close()
				if configured := slices.Contains(ctrl.ConfiguredMCPNames(), "ops"); configured != present {
					t.Fatalf("configured ops = %t, want %t", configured, present)
				}
				if err := ctrl.Run(t.Context(), "hello"); err != nil {
					t.Fatal(err)
				}
				if slices.Contains(requestToolNames(recorder.requests()[0]), "mcp__ops__deploy") {
					t.Fatal("default-load compatible server entered the provider schema")
				}
				if !activated && ctrl.Host().HasClient("ops") {
					t.Fatal("inactive compatible package connected before the explicit request")
				}
				if !present {
					_, err := ctrl.ConnectConfiguredMCPServer("ops")
					var missing *config.ServerNotFoundError
					if !errors.As(err, &missing) {
						t.Fatalf("absent package connection = %v, want ServerNotFoundError", err)
					}
					return
				}
				cfg, err := config.LoadForRootReadOnly(workspace)
				if err != nil {
					t.Fatal(err)
				}
				if len(cfg.Plugins) != 1 || cfg.Plugins[0].Type != "http" || cfg.Plugins[0].AutoStart == nil || *cfg.Plugins[0].AutoStart || cfg.Plugins[0].Source != config.MCPSourcePluginPackage {
					t.Fatalf("imported connection policy = %+v", cfg.Plugins)
				}
				if enabled, err := ctrl.MCPServerEnabled("ops"); err != nil || enabled != activated {
					t.Fatalf("MCP activation before connecting = %t, err=%v, want %t", enabled, err, activated)
				}
				if count, err := ctrl.ConnectConfiguredMCPServer("ops"); err != nil || count != 1 {
					t.Fatalf("explicit connection: tools=%d, err=%v", count, err)
				}
				if enabled, err := ctrl.MCPServerEnabled("ops"); err != nil || !enabled {
					t.Fatalf("MCP activation after connecting = %t, err=%v", enabled, err)
				}
				tools, err := ctrl.Host().ToolsFor(t.Context(), "ops")
				if err != nil || len(tools) != 1 || tools[0].Name() != "mcp__ops__deploy" {
					t.Fatalf("compatible package tools = %v, err=%v", tools, err)
				}
				owned, ok := tools[0].(interface{ MCPPackageName() string })
				if !ok || owned.MCPPackageName() != "compatible-ops" {
					t.Fatal("connected tool lost its compatible package owner")
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				result, err := tools[0].Execute(ctx, json.RawMessage(`{}`))
				if err != nil || !strings.Contains(result, "deployed") {
					t.Fatalf("compatible package call = %q, err=%v", result, err)
				}
				if err := ctrl.Run(t.Context(), "again"); err != nil {
					t.Fatal(err)
				}
				reqs := recorder.requests()
				before, err := json.Marshal(reqs[0].Tools)
				if err != nil {
					t.Fatal(err)
				}
				after, err := json.Marshal(reqs[len(reqs)-1].Tools)
				if err != nil || string(before) != string(after) {
					t.Fatalf("connection changed the provider tool schema: err=%v", err)
				}
			}
			check(true, false)
			check(true, true)
			if err := pluginpkg.SetEnabled(reasonixHome, "compatible-ops", false); err != nil {
				t.Fatal(err)
			}
			check(false, false)
			if err := pluginpkg.SetEnabled(reasonixHome, "compatible-ops", true); err != nil {
				t.Fatal(err)
			}
			check(true, true)
			if _, found, err := pluginpkg.Remove(reasonixHome, "compatible-ops"); err != nil || !found {
				t.Fatalf("remove package: found=%t, err=%v", found, err)
			}
			check(false, false)
		})
	}
}
