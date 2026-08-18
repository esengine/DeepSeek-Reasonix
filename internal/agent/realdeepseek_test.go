//go:build live

package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/anthropic"
	"reasonix/internal/tool"
)

func TestRealDeepSeekAgentInterruptedToolResume(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set — skipping live probe")
	}
	p := newLiveDeepSeekAgentProvider(t, key, false)
	marker := &liveMarkerTool{}
	reg := tool.NewRegistry()
	reg.Add(marker)
	sess := agent.NewSession("You are a concise tool-using assistant. Call the requested tool before answering.")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ToolResult && e.Tool.Name == marker.Name() {
			cancel()
		}
	})
	a := agent.New(p, reg, sess, agent.Options{MaxSteps: 4, MaxOutputTokens: 512}, sink)
	err := a.Run(ctx, "Call get_marker and report its result.")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted run error = %v, want context cancellation", err)
	}
	if marker.executions.Load() != 1 {
		t.Fatalf("tool executions before restart = %d, want 1", marker.executions.Load())
	}

	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := sess.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	reopened := agent.New(p, reg, reloaded, agent.Options{MaxSteps: 4, MaxOutputTokens: 512}, event.Discard)
	resumeCtx, resumeCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer resumeCancel()
	if err := reopened.Run(resumeCtx, "Continue from the completed tool result without calling the tool again. Reply with the marker."); err != nil {
		t.Fatalf("resume after restart: %v", err)
	}
	if marker.executions.Load() != 1 {
		t.Fatalf("old tool was executed again after restart: executions=%d", marker.executions.Load())
	}
	t.Logf("interrupted tool turn resumed after save/load with executions=%d messages=%d", marker.executions.Load(), len(reloaded.Snapshot()))
}

func TestRealDeepSeekAgentWebSearchContinuation(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set — skipping live probe")
	}
	p := newLiveDeepSeekAgentProvider(t, key, true)
	sess := agent.NewSession("You are a concise assistant. Use server-side web search when explicitly requested.")
	a := agent.New(p, tool.NewRegistry(), sess, agent.Options{MaxSteps: 3, MaxOutputTokens: 512}, event.Discard)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := a.Run(ctx, "Search the web for the latest DeepSeek API documentation update and reply with one source URL."); err != nil {
		t.Fatalf("web search turn: %v", err)
	}
	var found bool
	for _, m := range sess.Snapshot() {
		if m.Role == provider.RoleAssistant && len(m.ServerSearch) > 0 {
			found = true
			if strings.TrimSpace(m.ReasoningContent) == "" {
				t.Fatal("stored web-search turn lost provider reasoning")
			}
		}
	}
	if !found {
		t.Fatal("agent session contains no server-search assistant turn")
	}
	if err := a.Run(ctx, "Without searching again, reply with the hostname of that source."); err != nil {
		t.Fatalf("web search continuation: %v", err)
	}
	t.Logf("agent web-search continuation completed with messages=%d", len(sess.Snapshot()))
}

func newLiveDeepSeekAgentProvider(t *testing.T, key string, webSearch bool) provider.Provider {
	t.Helper()
	p, err := anthropic.New(provider.Config{
		Name: "deepseek-anthropic", BaseURL: "https://api.deepseek.com/anthropic", Model: "deepseek-v4-flash", APIKey: key,
		Extra: map[string]any{"api_key_env": "DEEPSEEK_API_KEY", "thinking": "enabled", "effort": "high", "web_search": webSearch},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if closer, ok := p.(interface{ CloseIdleConnections() }); ok {
		t.Cleanup(closer.CloseIdleConnections)
	}
	return p
}

type liveMarkerTool struct{ executions atomic.Int32 }

func (*liveMarkerTool) Name() string        { return "get_marker" }
func (*liveMarkerTool) Description() string { return "Return a fixed integration-test marker." }
func (*liveMarkerTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (m *liveMarkerTool) Execute(context.Context, json.RawMessage) (string, error) {
	m.executions.Add(1)
	return "protocol-round-trip-ok", nil
}
func (*liveMarkerTool) ReadOnly() bool { return true }
