package installsource

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func TestRevisionApprovalBindsOperationalCredentials(t *testing.T) {
	for _, field := range []string{"url", "headers", "env", "args"} {
		t.Run(field, func(t *testing.T) {
			root := testenv.TempDir(t)
			source := filepath.Join(root, ".mcp.json")
			connected := false
			var received config.PluginEntry
			stop := errors.New("fixture stop before persistence")
			tl := NewTool(Options{ProjectRoot: root, HomeDir: root, RequireApprovedPlan: true, ConnectMCP: func(entry config.PluginEntry) (MCPConnectResult, error) {
				connected = true
				received = entry
				return MCPConnectResult{}, stop
			}})
			write := func(secret string) {
				entry := map[string]any{"url": "https://host/mcp", "command": ""}
				switch field {
				case "url":
					entry["url"] = "https://host/mcp?token=" + secret
				case "headers":
					entry["headers"] = map[string]string{"Authorization": secret}
				case "env":
					entry["env"] = map[string]string{"PASSWORD": secret}
				case "args":
					delete(entry, "url")
					entry["command"] = "node"
					entry["args"] = []string{"--token", secret}
				}
				b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"neutral": entry}})
				writeFile(t, source, string(b))
			}
			write("fixturefirst")
			preview := execInstall(t, tl, map[string]any{"source": source, "kind": "mcp"})
			write("fixturesecond")
			updated := execInstall(t, tl, map[string]any{"source": source, "kind": "mcp"})
			if preview.PlanID == updated.PlanID {
				t.Error("credential change kept approval ticket")
			}
			raw, _ := json.Marshal(map[string]any{"source": source, "kind": "mcp", "apply": true, "planId": preview.PlanID})
			_, _ = tl.Execute(t.Context(), raw)
			if connected {
				t.Fatal("old ticket connected changed credentials")
			}
			raw, _ = json.Marshal(map[string]any{"source": source, "kind": "mcp", "apply": true, "planId": updated.PlanID})
			output, _ := tl.Execute(t.Context(), raw)
			if !connected {
				t.Fatal("current ticket did not connect")
			}
			operational, _ := json.Marshal(received)
			if !strings.Contains(string(operational), "fixturesecond") {
				t.Fatal("connector credential lost")
			}
			if strings.Contains(output, "fixturesecond") {
				t.Fatal("apply response leaked")
			}
		})
	}
}

func TestRevisionInstallSourceErrorMasksEndpoint(t *testing.T) {
	tl := NewTool(Options{ProjectRoot: testenv.TempDir(t), HomeDir: testenv.TempDir(t)})
	_, err := tl.Execute(t.Context(), json.RawMessage(`{"source":"https://host/mcp?%74oken=fixturesecret","kind":"skill"}`))
	if !errors.Is(err, ErrUnsupportedKind) {
		t.Fatalf("kind identity lost: %v", err)
	}
	if err == nil || strings.Contains(err.Error(), "fixturesecret") {
		t.Fatalf("source error leaked: %v", err)
	}
}
