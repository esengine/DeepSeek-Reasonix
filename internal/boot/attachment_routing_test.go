package boot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBuildDeepSeekTextParentCanUseImageReturningMCPAndVisionSubagent(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	registerBootSubagentTestProvider()
	prov := &bootSubagentTestProvider{combinedVision: true}
	setBootSubagentTestProvider(t, prov)
	if _, err := config.SetCredential("BOOT_DEEPSEEK_TEST_KEY", "test-key"); err != nil {
		t.Fatalf("store test DeepSeek credential: %v", err)
	}
	writeFile(t, dir, "reasonix.toml", `
default_model = "parent"

[agent]
system_prompt = "BASE"
subagent_model = "vision-model"
vision_model = "vision-model"

[[providers]]
name = "parent"
kind = "boot-subagent-test"
base_url = "https://api.deepseek.com/anthropic"
model = "x"
api_key_env = "BOOT_DEEPSEEK_TEST_KEY"

[[providers]]
name = "vision-model"
kind = "boot-subagent-test"
base_url = "https://vision.example.invalid"
model = "x"
vision = true
`)
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatalf("decode test png: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".reasonix", "attachments"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".reasonix", "attachments", "shot.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	mcpImage := base64.StdEncoding.EncodeToString(png)
	var mcpCalls atomic.Int32
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if request.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "vision-reader", "version": "1"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "inspect",
				"description": "Inspect an image file by path.",
				"inputSchema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"path": map[string]any{"type": "string"}},
					"required":   []string{"path"},
				},
				"annotations": map[string]any{"readOnlyHint": true},
			}}}
		case "tools/call":
			mcpCalls.Add(1)
			result = map[string]any{"content": []map[string]any{
				{"type": "text", "text": "vision-mcp-ok"},
				{"type": "image", "mimeType": "image/png", "data": mcpImage},
			}}
		default:
			http.Error(w, "unsupported method", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": result})
	}))
	defer mcpServer.Close()

	ctrl, err := Build(context.Background(), Options{
		Sink: event.Discard,
		ExtraPlugins: []plugin.Spec{{
			Name:       "vision-reader",
			Type:       "http",
			URL:        mcpServer.URL,
			Authorized: true,
		}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if ctrl.ImageInputEnabled() {
		loaded, loadErr := config.LoadForRoot(dir)
		resolved, _ := loaded.ResolveModel(ctrl.ModelRef())
		t.Fatalf("official DeepSeek parent unexpectedly enables direct images: ref=%q entry=%+v load_err=%v", ctrl.ModelRef(), resolved, loadErr)
	}
	if err := ctrl.Run(context.Background(), "review @.reasonix/attachments/shot.png"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := prov.requestsSnapshot()
	if len(reqs) < 4 {
		t.Fatalf("provider requests = %d, want parent MCP call, parent subagent call, vision child, and parent final", len(reqs))
	}
	if got := mcpCalls.Load(); got != 1 {
		t.Fatalf("vision MCP calls = %d, want 1", got)
	}
	// MCP tools stay behind use_capability; review is registered for dispatch.
	if !requestHasTool(reqs[0], "use_capability") {
		t.Fatalf("parent tools = %v, want use_capability for MCP/review dispatch", toolSchemaNames(reqs[0].Tools))
	}
	registered := map[string]bool{}
	for _, e := range ctrl.AllToolContractEntries() {
		registered[e.Name] = true
	}
	if !registered["review"] && !registered["run_skill"] {
		t.Fatalf("capability registry missing review/run_skill: %v", registered)
	}
	if got := bootLastUser(reqs[0]); !strings.Contains(got, "@.reasonix/attachments/shot.png") {
		t.Fatalf("text-only parent lost the attachment reference needed by MCP: %q", got)
	}
	// The text parent receives summaries; native images remain available to
	// the independently routed vision child.
	for _, requestIndex := range []int{0, 1, len(reqs) - 1} {
		for _, msg := range reqs[requestIndex].Messages {
			if msg.Role == provider.RoleUser && len(msg.Images) != 0 {
				t.Fatalf("text-only parent request %d embedded %d direct attachment(s): %+v", requestIndex, len(msg.Images), reqs[requestIndex].Messages)
			}
		}
	}
	if !requestMessageContains(reqs[1].Messages, provider.RoleTool, "vision-mcp-ok") {
		t.Fatalf("parent did not receive the vision MCP text result: %+v", reqs[1].Messages)
	}
	var parentMCPImageCount int
	for _, msg := range reqs[1].Messages {
		if msg.Role == provider.RoleTool && msg.Name == "mcp__vision-reader__inspect" {
			parentMCPImageCount += len(msg.Images)
		}
	}
	if parentMCPImageCount != 0 {
		t.Fatalf("text parent MCP result images = %d, want summary only", parentMCPImageCount)
	}
	var childImageCount int
	for _, msg := range reqs[2].Messages {
		if msg.Role == provider.RoleUser {
			childImageCount = len(msg.Images)
		}
	}
	if childImageCount != 1 {
		t.Fatalf("vision child user images = %d, want one attachment; request = %+v", childImageCount, reqs[2].Messages)
	}
}
