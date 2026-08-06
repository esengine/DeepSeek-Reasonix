//go:build live

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
	"reasonix/internal/tool"
)

// TestLiveDeepSeekFlashMissingReasoningRecovery exercises the production agent
// against DeepSeek's official API while a local proxy removes reasoning_content
// from one or two real tool-call responses. It is credential-gated and excluded
// from ordinary CI; response text, tool arguments, and credentials are never
// logged or written to disk.
func TestLiveDeepSeekFlashMissingReasoningRecovery(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}

	for _, tc := range []struct {
		name           string
		stripResponses int32
		wantRecovered  int
		wantFallback   int
	}{
		{name: "transient", stripResponses: 1, wantRecovered: 1},
		{name: "persistent", stripResponses: 2, wantFallback: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for attempt := 1; attempt <= 3; attempt++ {
				result := runLiveDeepSeekRecoveryScenario(t, key, tc.stripResponses, attempt)
				t.Logf("mode=%s attempt=%d upstream_requests=%d stripped_fields=%d retry_request_identical=%t executions=%d tool_turns=%d warnings=%d retry_attempts=%d recovered=%d replaced=%d fallbacks=%d",
					tc.name, attempt, result.requests, result.strippedFields, result.identicalRetry,
					result.executions, result.toolTurns, result.warnings, result.retryAttempts,
					result.recovered, result.replaced, result.fallbacks)
				if result.strippedFields == 0 || result.replaced != 0 {
					continue // provider chose a different response shape; retry a bounded fresh scenario
				}
				if result.executions != 1 || result.toolTurns != 1 {
					t.Fatalf("tool execution/session turns = %d/%d, want 1/1", result.executions, result.toolTurns)
				}
				if result.warnings != 0 {
					t.Fatalf("user-visible protocol warnings = %d, want 0", result.warnings)
				}
				if result.retryAttempts == 0 {
					if result.recovered != 0 || result.fallbacks != 1 {
						t.Fatalf("no-retry fallback outcomes recovered/fallbacks = %d/%d, want 0/1",
							result.recovered, result.fallbacks)
					}
					continue // visible text made retry unsafe; fallback was correct, seek the requested retry shape
				}
				if !result.identicalRetry {
					t.Fatal("missing-reasoning recovery changed the provider request")
				}
				if result.retryAttempts != 1 || result.recovered != tc.wantRecovered || result.fallbacks != tc.wantFallback {
					t.Fatalf("recovery outcomes attempts/recovered/fallbacks = %d/%d/%d, want 1/%d/%d",
						result.retryAttempts, result.recovered, result.fallbacks, tc.wantRecovered, tc.wantFallback)
				}
				return
			}
			t.Fatal("official API did not produce the requested live recovery shape in three bounded attempts")
		})
	}
}

type liveRecoveryResult struct {
	requests, strippedFields           int
	executions, toolTurns, warnings    int
	retryAttempts, recovered, replaced int
	fallbacks                          int
	identicalRetry                     bool
}

func runLiveDeepSeekRecoveryScenario(t *testing.T, key string, stripResponses int32, attempt int) liveRecoveryResult {
	t.Helper()
	proxy := &liveReasoningStripProxy{stripResponses: stripResponses}
	server := httptest.NewServer(proxy)
	defer server.Close()

	prov, err := openai.New(provider.Config{
		Name:    "deepseek-live-recovery",
		BaseURL: server.URL,
		Model:   "deepseek-v4-flash",
		APIKey:  key,
		Extra: map[string]any{
			"api_key_env":        "DEEPSEEK_API_KEY",
			"reasoning_protocol": "deepseek",
			"thinking":           "enabled",
			"effort":             "low",
		},
	})
	if err != nil {
		t.Fatalf("create live provider: %v", err)
	}

	var executions atomic.Int32
	registry := tool.NewRegistry()
	registry.Add(liveRecoveryEchoTool{executions: &executions})
	sink := &recordSink{}
	a := New(prov, registry, NewSession("You are a concise tool-using assistant."), Options{
		MaxSteps:                     4,
		MissingReasoningWarnStateDir: t.TempDir(),
	}, sink)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := a.Run(ctx, fmt.Sprintf("Live recovery probe %d: call echo exactly once, then report that it completed.", attempt)); err != nil {
		t.Fatalf("live agent run: %v", err)
	}

	result := liveRecoveryResult{executions: int(executions.Load())}
	for _, msg := range a.Session().Snapshot() {
		if msg.Role == provider.RoleAssistant && len(msg.ToolCalls) > 0 {
			result.toolTurns++
		}
	}
	for _, notice := range sink.kinds(event.Notice) {
		text := strings.ToLower(notice.Text + " " + notice.Detail)
		if strings.Contains(text, "reasoning_content") || strings.Contains(text, "replayable thinking") {
			result.warnings++
		}
	}
	result.retryAttempts = sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryAttempted)
	result.recovered = sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryRecovered)
	result.replaced = sink.recoveryCount(event.ProtocolRecoveryMissingReasoningRetryReplaced)
	result.fallbacks = sink.recoveryCount(event.ProtocolRecoveryMissingReasoningFallback)
	result.requests = int(proxy.requests.Load())
	result.strippedFields = int(proxy.strippedFields.Load())
	proxy.mu.Lock()
	result.identicalRetry = len(proxy.firstBody) > 0 && bytes.Equal(proxy.firstBody, proxy.retryBody)
	proxy.mu.Unlock()
	return result
}

type liveRecoveryEchoTool struct{ executions *atomic.Int32 }

func (t liveRecoveryEchoTool) Name() string        { return "echo" }
func (t liveRecoveryEchoTool) Description() string { return "Return a fixed live-test marker." }
func (t liveRecoveryEchoTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (t liveRecoveryEchoTool) ReadOnly() bool { return true }
func (t liveRecoveryEchoTool) Execute(context.Context, json.RawMessage) (string, error) {
	t.executions.Add(1)
	return "live recovery marker", nil
}

type liveReasoningStripProxy struct {
	stripResponses int32
	requests       atomic.Int32
	toolResponses  atomic.Int32
	strippedFields atomic.Int32
	mu             sync.Mutex
	firstRequestNo int32
	firstBody      []byte
	retryBody      []byte
}

func (p *liveReasoningStripProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read request", http.StatusBadRequest)
		return
	}
	requestNo := p.requests.Add(1)
	p.mu.Lock()
	if p.firstRequestNo != 0 && requestNo == p.firstRequestNo+1 {
		p.retryBody = append([]byte(nil), body...)
	}
	p.mu.Unlock()

	upstream, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		"https://api.deepseek.com/chat/completions", bytes.NewReader(body))
	if err != nil {
		http.Error(w, "create upstream request", http.StatusInternalServerError)
		return
	}
	upstream.Header.Set("Authorization", r.Header.Get("Authorization"))
	upstream.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(upstream)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "read upstream response", http.StatusBadGateway)
		return
	}
	if resp.StatusCode == http.StatusOK && bytes.Contains(responseBody, []byte(`"tool_calls"`)) {
		toolResponse := p.toolResponses.Add(1)
		if toolResponse <= p.stripResponses {
			if toolResponse == 1 {
				p.mu.Lock()
				p.firstRequestNo = requestNo
				p.firstBody = append([]byte(nil), body...)
				p.mu.Unlock()
			}
			responseBody = p.stripReasoning(responseBody)
		}
	}
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(responseBody)
}

func (p *liveReasoningStripProxy) stripReasoning(body []byte) []byte {
	lines := bytes.Split(body, []byte("\n"))
	for i, line := range lines {
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data: ")))
		if bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		var payload map[string]any
		if json.Unmarshal(data, &payload) != nil {
			continue
		}
		choices, _ := payload["choices"].([]any)
		changed := false
		for _, rawChoice := range choices {
			choice, _ := rawChoice.(map[string]any)
			delta, _ := choice["delta"].(map[string]any)
			if _, ok := delta["reasoning_content"]; ok {
				delete(delta, "reasoning_content")
				p.strippedFields.Add(1)
				changed = true
			}
		}
		if changed {
			encoded, err := json.Marshal(payload)
			if err == nil {
				lines[i] = append([]byte("data: "), encoded...)
			}
		}
	}
	return bytes.Join(lines, []byte("\n"))
}
