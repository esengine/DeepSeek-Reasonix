//go:build live

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
	"reasonix/internal/tool"
)

type liveLargeResultTool struct{}

func (liveLargeResultTool) Name() string { return "live_large_result" }
func (liveLargeResultTool) Description() string {
	return "Return the fixed large cache-regression fixture. Call only when the user explicitly requests it."
}
func (liveLargeResultTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (liveLargeResultTool) ReadOnly() bool { return true }
func (liveLargeResultTool) Execute(context.Context, json.RawMessage) (string, error) {
	const sentinel = "LIVE-RAW-MIDDLE-SENTINEL"
	return strings.Repeat("R", 128<<10) + sentinel + strings.Repeat("R", (128<<10)-len(sentinel)), nil
}

type liveCacheCaptureProvider struct {
	provider.Provider
	mu       sync.Mutex
	requests []provider.Request
}

func (p *liveCacheCaptureProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	copyReq := req
	copyReq.Messages = append([]provider.Message(nil), req.Messages...)
	copyReq.Tools = append([]provider.ToolSchema(nil), req.Tools...)
	p.requests = append(p.requests, copyReq)
	p.mu.Unlock()
	return p.Provider.Stream(ctx, req)
}

func (p *liveCacheCaptureProvider) snapshot() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]provider.Request(nil), p.requests...)
}

func TestLiveDeepSeekLargeToolResultCacheTrend(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}
	baseURL := strings.TrimRight(os.Getenv("DEEPSEEK_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	base, err := openai.New(provider.Config{
		Name: "live-tool-cache", BaseURL: baseURL, Model: "deepseek-v4-flash", APIKey: key,
		Extra: map[string]any{"api_key_env": "DEEPSEEK_API_KEY"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if closer, ok := base.(interface{ CloseIdleConnections() }); ok {
		t.Cleanup(closer.CloseIdleConnections)
	}
	capture := &liveCacheCaptureProvider{Provider: base}
	reg := tool.NewRegistry()
	reg.Add(liveLargeResultTool{})
	var usages []*provider.Usage
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Usage && e.Usage != nil {
			copyUsage := *e.Usage
			usages = append(usages, &copyUsage)
		}
	})
	system := "You are a concise cache-regression agent. Follow explicit tool instructions. " +
		strings.Repeat("Keep this stable provider prefix byte-identical across every request. ", 80)
	a := New(capture, reg, NewSession(system), Options{ContextWindow: 1_000_000, MaxOutputTokens: 256, MaxSteps: 5}, sink)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	turns := []string{
		"Call live_large_result exactly once, then answer only: received.",
		"Do not call tools. Reply only: second.",
		"Do not call tools. Reply only: third.",
	}
	for i, prompt := range turns {
		if err := a.Run(ctx, prompt); err != nil {
			t.Fatalf("turn %d: %v", i+1, err)
		}
	}

	requests := capture.snapshot()
	if len(requests) < 4 {
		t.Fatalf("captured requests=%d, want tool loop plus two ordinary turns", len(requests))
	}
	seenBoundedTool := false
	for i, req := range requests {
		wire, err := json.Marshal(req)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(wire, []byte("LIVE-RAW-MIDDLE-SENTINEL")) {
			t.Fatalf("request %d leaked local RawContent sentinel", i+1)
		}
		for _, msg := range req.Messages {
			if msg.Role == provider.RoleTool && msg.Name == "live_large_result" {
				seenBoundedTool = true
				if msg.RawContent != "" || len(msg.Content) > maxToolOutputBytes {
					t.Fatalf("request %d tool bytes: content=%d raw=%d", i+1, len(msg.Content), len(msg.RawContent))
				}
			}
		}
		if i > 0 {
			previous := requests[i-1].Messages
			current := req.Messages
			if len(current) >= len(previous) {
				for j := range previous {
					before, _ := json.Marshal(previous[j])
					after, _ := json.Marshal(current[j])
					if !bytes.Equal(before, after) {
						t.Fatalf("request %d changed prior provider message %d", i+1, j)
					}
				}
			}
		}
	}
	if !seenBoundedTool {
		t.Fatal("model did not execute the large-result fixture")
	}
	for i, usage := range usages {
		t.Logf("live cache turn=%d prompt=%d hit=%d miss=%d", i+1, usage.PromptTokens, usage.CacheHitTokens, usage.CacheMissTokens)
	}
}
