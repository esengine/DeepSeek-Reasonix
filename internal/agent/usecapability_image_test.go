package agent

import (
	"context"
	"encoding/base64"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/capability"
	"reasonix/internal/event"
	"reasonix/internal/imageinput"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestPlannerFirstOnDemandMCPCallPreservesImages(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	payload := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	var toolCalls atomic.Int32
	server := imageMCPServer(t, &toolCalls, payload)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host := plugin.NewHost()
	defer host.Close()
	spec := plugin.Spec{Name: "image", Type: "http", URL: server.URL, Authorized: true}
	runtime := NewMCPCapabilityRuntime(ctx, host, []plugin.Spec{spec}, tool.NewRegistry(), nil)
	proxy := runtime.NewFrontend(capability.NewLedger(), nil)
	reg := tool.NewRegistry()
	reg.Add(proxy)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("image-call", "use_capability", `{"action":"call","capability_id":"mcp-tool:image/screenshot","arguments":{}}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	session := NewSession("sys")
	vision := &summaryProvider{}
	planner := NewPlannerAgent(prov, reg, session, Options{ImageInput: &imageinput.Config{Model: "vision/model", Resolve: func(string) (provider.Provider, error) { return vision, nil }}}, event.Discard)
	if host.HasClient("image") {
		t.Fatal("test requires the MCP server to start on first tool dispatch")
	}
	if err := planner.Run(withNoClosedLoop(ctx), "take a screenshot"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := toolCalls.Load(); got != 1 {
		t.Fatalf("image tools/call count = %d, want 1", got)
	}
	wantImage := "data:image/png;base64," + payload
	for _, message := range session.Messages {
		if message.Role != provider.RoleTool || message.ToolCallID != "image-call" {
			continue
		}
		if len(message.Images) != 1 || message.Images[0] != wantImage {
			t.Fatalf("first on-demand MCP images = %v, want %q", message.Images, wantImage)
		}
		if !strings.Contains(message.Content, "captured [image: image/png]") {
			t.Fatalf("first on-demand MCP text = %q, want image placeholder", message.Content)
		}
		if message.VisionSummary == nil || !strings.Contains(message.Content, "OCR: Z7") || vision.calls.Load() != 1 {
			t.Fatal("on-demand image did not use summary service")
		}
		return
	}
	t.Fatal("no tool message recorded for first on-demand MCP call")
}
