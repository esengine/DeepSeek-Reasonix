//go:build live

package agent

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/anthropic"
	"reasonix/internal/provider/openai"
	"reasonix/internal/provider/responses"
	"reasonix/internal/tool"
)

// TestLiveOpenCodeGoDeepSeekAgentToolLoops repeatedly exercises the production
// Agent recovery boundary against both strict OpenCode Go Flash protocols.
func TestLiveOpenCodeGoDeepSeekAgentToolLoops(t *testing.T) {
	key := os.Getenv("OPENCODE_GO_API_KEY")
	if key == "" {
		t.Skip("OPENCODE_GO_API_KEY not set")
	}
	providers := []struct {
		name string
		new  func() (provider.Provider, error)
	}{
		{name: "anthropic", new: func() (provider.Provider, error) {
			return anthropic.New(provider.Config{
				Name: "opencode-go-deepseek-anthropic", BaseURL: "https://opencode.ai/zen/go", Model: "deepseek-v4-flash", APIKey: key,
				Extra: map[string]any{
					"api_key_env": "OPENCODE_GO_API_KEY", "reasoning_protocol": "deepseek",
					"thinking": "adaptive", "effort": "high", "web_search": true,
				},
			})
		}},
		{name: "responses", new: func() (provider.Provider, error) {
			return responses.New(responses.Config{
				Name: "opencode-go-deepseek-responses", BaseURL: "https://opencode.ai/zen/go/v1", Model: "deepseek-v4-flash",
				APIKey: key, KeyEnv: "OPENCODE_GO_API_KEY", Effort: "high", Mode: "stateless", WebSearch: true, MaxOutputTokens: 512,
			}), nil
		}},
	}

	for _, tc := range providers {
		t.Run(tc.name, func(t *testing.T) {
			prov, err := tc.new()
			if err != nil {
				t.Fatalf("new provider: %v", err)
			}
			if closer, ok := prov.(interface{ CloseIdleConnections() }); ok {
				t.Cleanup(closer.CloseIdleConnections)
			}
			retries, recovered := runLiveAgentToolLoops(t, prov, 10)
			t.Logf("protocol=%s runs=10 tool_executions=10 retry_attempts=%d recovered=%d", tc.name, retries, recovered)
		})
	}
}

func TestLiveCompatibleProviderDefaultReasoningAgentToolLoops(t *testing.T) {
	providers := []struct {
		name, keyEnv, baseURL, model string
		extra                        map[string]any
	}{
		{name: "longcat", keyEnv: "LONGCAT_API_KEY", baseURL: "https://api.longcat.chat/openai/v1", model: "LongCat-2.0", extra: map[string]any{"thinking": "enabled", "effort": "enabled"}},
		{name: "zhipu-coding-plan", keyEnv: "GLM_PLAN_API_KEY", baseURL: "https://open.bigmodel.cn/api/coding/paas/v4", model: "glm-5.2", extra: map[string]any{"reasoning_protocol": "glm"}},
	}
	for _, tc := range providers {
		t.Run(tc.name, func(t *testing.T) {
			key := os.Getenv(tc.keyEnv)
			if key == "" {
				t.Skip(tc.keyEnv + " not set")
			}
			tc.extra["api_key_env"] = tc.keyEnv
			prov, err := openai.New(provider.Config{Name: tc.name, BaseURL: tc.baseURL, Model: tc.model, APIKey: key, Extra: tc.extra})
			if err != nil {
				t.Fatalf("new provider: %v", err)
			}
			if closer, ok := prov.(interface{ CloseIdleConnections() }); ok {
				t.Cleanup(closer.CloseIdleConnections)
			}
			retries, recovered := runLiveAgentToolLoops(t, prov, 5)
			t.Logf("provider=%s runs=5 tool_executions=5 retry_attempts=%d recovered=%d", tc.name, retries, recovered)
		})
	}
}

func runLiveAgentToolLoops(t *testing.T, prov provider.Provider, runs int) (retries, recovered int) {
	t.Helper()
	stateDir := t.TempDir()
	for attempt := 1; attempt <= runs; attempt++ {
		var executions atomic.Int32
		registry := tool.NewRegistry()
		registry.Add(liveRecoveryEchoTool{executions: &executions})
		sink := &recordSink{}
		session := NewSession("You are a concise tool-using assistant. Call the requested tool exactly once before answering.")
		a := New(prov, registry, session, Options{
			MaxSteps: 4, MaxOutputTokens: 512, MissingReasoningWarnStateDir: stateDir,
		}, sink)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		err := a.Run(ctx, "Call echo exactly once, then reply with the marker result.")
		cancel()
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if got := executions.Load(); got != 1 {
			t.Fatalf("attempt %d tool executions = %d, want 1", attempt, got)
		}
		messages := session.Snapshot()
		if len(messages) == 0 || strings.TrimSpace(messages[len(messages)-1].Content) == "" {
			t.Fatalf("attempt %d produced no final assistant text", attempt)
		}
		retries += sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted)
		recovered += sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryRecovered)
	}
	return retries, recovered
}
