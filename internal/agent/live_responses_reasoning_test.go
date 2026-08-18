//go:build live

package agent

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/provider/responses"
)

// TestLiveDeepSeekResponsesAgentToolLoops verifies the official stateless
// endpoint accepts tool turns both with and without a reasoning item. Every
// run must execute the client tool exactly once and reach a visible final.
func TestLiveDeepSeekResponsesAgentToolLoops(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}
	for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-pro"} {
		t.Run(model, func(t *testing.T) {
			prov := responses.New(responses.Config{
				Name: "deepseek-responses", BaseURL: "https://api.deepseek.com", Model: model,
				APIKey: key, KeyEnv: "DEEPSEEK_API_KEY", Effort: "high", Mode: "stateless", MaxOutputTokens: 512,
			})
			if closer, ok := prov.(interface{ CloseIdleConnections() }); ok {
				t.Cleanup(closer.CloseIdleConnections)
			}
			retries, recovered := runLiveAgentToolLoops(t, prov, 10)
			t.Logf("model=%s runs=10 tool_executions=10 retry_attempts=%d recovered=%d", model, retries, recovered)
		})
	}
}

// TestLiveDeepSeekResponsesMissingReasoningFallback keeps the upstream model
// and stream real while a localhost proxy removes provider reasoning events
// from tool-call responses. This deterministically exercises the compatibility
// fallback without logging model output, tool arguments, or credentials.
func TestLiveDeepSeekResponsesMissingReasoningFallback(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}
	for _, tc := range []struct {
		model          string
		stripResponses int32
		wantRetries    int
	}{
		{model: "deepseek-v4-flash", stripResponses: 1},
		{model: "deepseek-v4-pro", stripResponses: 2, wantRetries: 1},
	} {
		t.Run(tc.model, func(t *testing.T) {
			proxy := &liveResponsesReasoningStripProxy{stripResponses: tc.stripResponses}
			server := httptest.NewServer(proxy)
			defer server.Close()
			prov := responses.New(responses.Config{
				Name: "deepseek-responses", BaseURL: "https://api.deepseek.com", RequestURL: server.URL,
				Model: tc.model, APIKey: key, KeyEnv: "DEEPSEEK_API_KEY", Effort: "high", Mode: "stateless", MaxOutputTokens: 512,
			})
			if closer, ok := prov.(interface{ CloseIdleConnections() }); ok {
				t.Cleanup(closer.CloseIdleConnections)
			}
			retries, recovered := runLiveAgentToolLoops(t, prov, 1)
			if retries != tc.wantRetries {
				t.Fatalf("reasoning retries = %d, want %d", retries, tc.wantRetries)
			}
			if proxy.toolResponses.Load() < tc.stripResponses {
				t.Fatalf("tool responses = %d, want at least %d", proxy.toolResponses.Load(), tc.stripResponses)
			}
			if tc.wantRetries > 0 && !proxy.firstTwoToolRequestsEqual() {
				t.Fatal("missing-reasoning retry changed the frozen request")
			}
			t.Logf("model=%s upstream_requests=%d tool_responses=%d stripped_events=%d retry_attempts=%d recovered=%d exact_retry=%t",
				tc.model, proxy.requests.Load(), proxy.toolResponses.Load(), proxy.strippedEvents.Load(), retries, recovered,
				proxy.firstTwoToolRequestsEqual())
		})
	}
}

type liveResponsesReasoningStripProxy struct {
	stripResponses int32
	requests       atomic.Int32
	toolResponses  atomic.Int32
	strippedEvents atomic.Int32
	mu             sync.Mutex
	toolBodies     [][]byte
}

func (p *liveResponsesReasoningStripProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read request", http.StatusBadRequest)
		return
	}
	p.requests.Add(1)
	upstream, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		"https://api.deepseek.com/responses", bytes.NewReader(body))
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
	if resp.StatusCode == http.StatusOK && bytes.Contains(responseBody, []byte(`"type":"function_call"`)) {
		toolResponse := p.toolResponses.Add(1)
		if toolResponse <= p.stripResponses {
			p.mu.Lock()
			p.toolBodies = append(p.toolBodies, append([]byte(nil), body...))
			p.mu.Unlock()
			var stripped int
			responseBody, stripped = stripResponsesReasoningEvents(responseBody)
			p.strippedEvents.Add(int32(stripped))
		}
	}
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(responseBody)
}

func (p *liveResponsesReasoningStripProxy) firstTwoToolRequestsEqual() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.toolBodies) >= 2 && bytes.Equal(p.toolBodies[0], p.toolBodies[1])
}

func stripResponsesReasoningEvents(body []byte) ([]byte, int) {
	lines := bytes.Split(body, []byte("\n"))
	out := make([][]byte, 0, len(lines))
	stripped := 0
	for _, line := range lines {
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if bytes.Equal(data, line) || len(data) == 0 {
			out = append(out, line)
			continue
		}
		if bytes.Contains(data, []byte(`"type":"response.reasoning`)) ||
			(bytes.Contains(data, []byte(`"type":"response.output_item`)) && bytes.Contains(data, []byte(`"type":"reasoning"`))) {
			if len(out) > 0 && bytes.HasPrefix(bytes.TrimSpace(out[len(out)-1]), []byte("event:")) {
				out = out[:len(out)-1]
			}
			stripped++
			continue
		}
		out = append(out, line)
	}
	return bytes.Join(out, []byte("\n")), stripped
}
