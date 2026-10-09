package boot

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
)

func TestEffectExportedPluginMCPHeadersUseRecipientEnvironment(t *testing.T) {
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	writeFile(t, workspace, "reasonix.toml", "[environment]\nenabled = false\n[codegraph]\nenabled = false\n")
	approveWorkspace(t, workspace)
	const existingVar = "REASONIX_EXPORT_EFFECT_EXISTING"
	t.Setenv(existingVar, "author-existing")
	type observedRequest struct{ method, authorization, existing string }
	var mu sync.Mutex
	var observed []observedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		mu.Lock()
		observed = append(observed, observedRequest{request.Method, r.Header.Get("Authorization"), r.Header.Get("X-Existing")})
		mu.Unlock()
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "fixture", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "inspect", "description": "Inspect fixture transport.", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "fixture reached"}}}
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": result})
	}))
	defer server.Close()
	source := robustTempDir(t)
	manifest, err := json.Marshal(map[string]any{
		"apiVersion": "reasonix.io/plugin/v2", "name": "export-http", "version": "1.0.0",
		"contributes": map[string]any{"mcpServers": map[string]any{"export_probe": map[string]any{
			"type": "http", "url": server.URL, "load": "always",
			"headers": map[string]string{"Authorization": "Bearer author-fixture", "X-Existing": "${" + existingVar + "}"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, source, pluginpkg.NativeManifest, string(manifest))
	installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
	install := func(args map[string]any) string {
		t.Helper()
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		out, err := installer.Execute(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			OK      bool   `json:"ok"`
			Applied bool   `json:"applied"`
			Status  string `json:"status"`
			PlanID  string `json:"planId"`
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil || !result.OK {
			t.Fatalf("install_source = %s, err=%v", out, err)
		}
		if args["apply"] == true || args["op"] == "uninstall" {
			if !result.Applied || result.Status != "done" {
				t.Fatalf("installation did not apply: %s", out)
			}
		} else if result.Applied || result.Status != "planned" || result.PlanID == "" {
			t.Fatalf("installation did not preview: %s", out)
		}
		return result.PlanID
	}
	runPhase := func(t *testing.T, authorization, existing string) {
		t.Helper()
		mu.Lock()
		observed = nil
		mu.Unlock()
		ctrl, err := Build(t.Context(), Options{Sink: event.Discard})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			ctrl.Close()
			if authorization == "" {
				mu.Lock()
				requests := slices.Clone(observed)
				mu.Unlock()
				if len(requests) != 0 {
					t.Fatalf("absent package sent MCP requests: %+v", requests)
				}
			}
		}()
		if authorization == "" {
			if _, err := ctrl.Host().ToolsFor(t.Context(), "export_probe"); err == nil {
				t.Fatal("absent package exposes its MCP server")
			}
			return
		}
		waitForMCPServer(t, ctrl.Host(), "export_probe")
		tools, err := ctrl.Host().ToolsFor(t.Context(), "export_probe")
		if err != nil || len(tools) != 1 || tools[0].Name() != "mcp__export_probe__inspect" {
			t.Fatalf("installed MCP tools = %v, err=%v", tools, err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		out, err := tools[0].Execute(ctx, json.RawMessage(`{}`))
		if err != nil || !strings.Contains(out, "fixture reached") {
			t.Fatalf("MCP call = %q, err=%v", out, err)
		}
		mu.Lock()
		requests := slices.Clone(observed)
		mu.Unlock()
		var methods []string
		for _, request := range requests {
			if request.authorization != authorization || request.existing != existing {
				t.Fatalf("MCP request = %+v, want recipient headers %q and %q", request, authorization, existing)
			}
			methods = append(methods, request.method)
		}
		for _, method := range []string{"initialize", "tools/list", "tools/call"} {
			if !slices.Contains(methods, method) {
				t.Fatalf("observed methods = %v, missing %s", methods, method)
			}
		}
	}
	args := map[string]any{"source": source, "kind": "plugin", "mode": "copy", "scope": "global"}
	args["planId"] = install(args)
	t.Run("source-preview", func(t *testing.T) { runPhase(t, "", "") })
	args["apply"] = true
	install(args)
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	t.Run("author-install", func(t *testing.T) { runPhase(t, "Bearer author-fixture", "author-existing") })
	root := pluginpkg.InstallRoot(reasonixHome, "export-http")
	archive, required, err := pluginpkg.Export("export-http", root)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	f, err := zr.Open("export-http/" + pluginpkg.NativeManifest)
	if err != nil {
		t.Fatal(err)
	}
	exported, err := io.ReadAll(f)
	if closeErr := f.Close(); err != nil || closeErr != nil {
		t.Fatalf("read exported manifest: %v, close: %v", err, closeErr)
	}
	var pkg struct {
		Contributes struct {
			MCPServers map[string]struct {
				Headers map[string]string `json:"headers"`
			} `json:"mcpServers"`
		} `json:"contributes"`
	}
	if err := json.Unmarshal(exported, &pkg); err != nil {
		t.Fatal(err)
	}
	headers := pkg.Contributes.MCPServers["export_probe"].Headers
	reference := headers["Authorization"]
	if !strings.HasPrefix(reference, "${") || !strings.HasSuffix(reference, "}") || bytes.Contains(exported, []byte("author-fixture")) {
		t.Fatalf("exported Authorization = %q, want a reference without the author value", reference)
	}
	if headers["X-Existing"] != "${"+existingVar+"}" {
		t.Fatalf("exported X-Existing = %q, want its existing reference", headers["X-Existing"])
	}
	variable := strings.TrimSuffix(strings.TrimPrefix(reference, "${"), "}")
	if len(required) != 2 || !slices.Contains(required, variable) || !slices.Contains(required, existingVar) {
		t.Fatalf("required variables = %v, want the generated header variable and %s", required, existingVar)
	}
	zipPath := filepath.Join(robustTempDir(t), "export-http.zip")
	if err := os.WriteFile(zipPath, archive, 0o644); err != nil {
		t.Fatal(err)
	}
	install(map[string]any{"op": "uninstall", "kind": "plugin", "name": "export-http", "scope": "global"})
	t.Setenv(variable, "Bearer recipient-fixture")
	t.Setenv(existingVar, "recipient-existing")
	args = map[string]any{"source": zipPath, "kind": "plugin", "scope": "global"}
	args["planId"] = install(args)
	t.Run("archive-preview", func(t *testing.T) { runPhase(t, "", "") })
	args["apply"] = true
	install(args)
	if err := os.Remove(zipPath); err != nil {
		t.Fatal(err)
	}
	t.Run("recipient-install", func(t *testing.T) { runPhase(t, "Bearer recipient-fixture", "recipient-existing") })
	if err := pluginpkg.SetEnabled(reasonixHome, "export-http", false); err != nil {
		t.Fatal(err)
	}
	t.Run("disabled", func(t *testing.T) { runPhase(t, "", "") })
	if err := pluginpkg.SetEnabled(reasonixHome, "export-http", true); err != nil {
		t.Fatal(err)
	}
	t.Run("reenabled", func(t *testing.T) { runPhase(t, "Bearer recipient-fixture", "recipient-existing") })
	install(map[string]any{"op": "uninstall", "kind": "plugin", "name": "export-http", "scope": "global"})
	t.Run("removed", func(t *testing.T) { runPhase(t, "", "") })
}
