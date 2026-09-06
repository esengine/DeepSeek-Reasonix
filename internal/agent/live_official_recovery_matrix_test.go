//go:build live

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"reasonix/internal/provider/anthropic"
	"reasonix/internal/provider/openai"
	"reasonix/internal/provider/responses"
	"reasonix/internal/tool"
)

// Only synthetic echo is exposed: no shell, filesystem reader, MCP, or access
// to credentials. Faults affect real completed upstream streams locally, not
// the public service. Raw requests/responses remain in memory.
func TestLiveOfficialRecoveryMatrix(t *testing.T) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}
	for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-pro"} {
		for _, protocol := range []string{"chat", "responses", "anthropic"} {
			for _, scenario := range []string{"low", "max", "disabled", "cut_once", "missing_once", "missing_persistent", "followup_503", "server_replay_rejection", "cancel_before_commit"} {
				t.Run(model+"/"+protocol+"/"+scenario, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
					defer cancel()
					proxy := &officialRecoveryProxy{protocol: protocol, scenario: scenario, cancel: cancel}
					srv := httptest.NewServer(proxy)
					defer srv.Close()
					effort := "high"
					if scenario == "low" || scenario == "max" || scenario == "disabled" {
						effort = scenario
					}
					p := officialMatrixProvider(t, key, model, protocol, effort, srv.URL)
					var executions atomic.Int32
					reg := tool.NewRegistry()
					reg.Add(liveRecoveryEchoTool{executions: &executions})
					sink := &recordSink{}
					session := NewSession("You are a concise tool-using assistant. Call echo exactly once when asked, then report the marker. Do not repeat a completed tool.")
					a := New(p, reg, session, Options{MaxSteps: 4, MaxOutputTokens: 2048, MissingReasoningWarnStateDir: t.TempDir()}, sink)
					start := time.Now()
					err := a.Run(ctx, "Call echo exactly once, then report its result.")
					wantRequests, wantExecutions := 2, int32(1)
					switch scenario {
					case "cut_once", "followup_503", "server_replay_rejection":
						wantRequests = 3
					case "missing_once":
						if protocol == "anthropic" {
							wantRequests = 3
						}
					case "missing_persistent":
						if protocol == "anthropic" {
							wantExecutions = 0
							if err == nil {
								t.Fatal("strict missing proof accepted")
							}
						}
					case "cancel_before_commit":
						wantRequests, wantExecutions = 1, 0
						if !errors.Is(err, context.Canceled) {
							t.Fatalf("cancellation error=%v", err)
						}
					}
					stopped := scenario == "cancel_before_commit" || scenario == "missing_persistent" && protocol == "anthropic"
					if !stopped && err != nil {
						t.Fatalf("live run: %v", err)
					}
					if scenario == "server_replay_rejection" && err == nil {
						t.Logf("before_continuation_tool_executions=%d", executions.Load())
						if err := a.Run(ctx, "Without calling tools, report the already known marker from the completed request."); err != nil {
							t.Fatalf("post-repair continuation: %v", err)
						}
						wantRequests = 4
					}
					proxy.mu.Lock()
					requests, upstream, mutations := proxy.requests, proxy.upstream, proxy.mutations
					frozen := len(proxy.bodies) > 1 && bytes.Equal(proxy.bodies[0], proxy.bodies[1])
					proxy.mu.Unlock()
					if requests != wantRequests || executions.Load() != wantExecutions {
						t.Fatalf("requests=%d executions=%d want=%d/%d err=%v", requests, executions.Load(), wantRequests, wantExecutions, err)
					}
					if strings.HasPrefix(scenario, "missing") || scenario == "cut_once" {
						if mutations == 0 {
							t.Fatal("requested fault was not injected")
						}
					}
					if scenario == "cut_once" && !frozen {
						t.Fatal("stream retry did not reuse frozen request")
					}
					if !stopped {
						msgs := session.Snapshot()
						if len(msgs) == 0 || strings.TrimSpace(msgs[len(msgs)-1].Content) == "" {
							t.Fatal("no final text")
						}
					}
					prompt, completion, accounted, unknown := 0, 0, 0, false
					for _, e := range sink.kinds(event.Usage) {
						if u := e.Usage; u != nil {
							prompt += u.PromptTokens
							completion += u.CompletionTokens
							accounted += u.RequestCount
							unknown = unknown || u.Unknown
						}
					}
					if (scenario == "cut_once" || scenario == "cancel_before_commit" || scenario == "followup_503" || scenario == "server_replay_rejection") && !unknown {
						t.Fatal("missing terminal usage must stay unknown")
					}
					if accounted != requests {
						t.Fatalf("accounted=%d HTTP=%d", accounted, requests)
					}
					t.Logf("protocol=%s scenario=%s http_attempts=%d upstream_requests=%d mutations=%d tool_executions=%d retry_events=%d prompt=%d completion=%d accounted_requests=%d usage_unknown=%t elapsed_ms=%d", protocol, scenario, requests, upstream, mutations, executions.Load(), len(sink.kinds(event.Retrying)), prompt, completion, accounted, unknown, time.Since(start).Milliseconds())
				})
			}
		}
	}
}

func officialMatrixProvider(t *testing.T, key, model, protocol, effort, url string) provider.Provider {
	t.Helper()
	extra := map[string]any{"api_key_env": "DEEPSEEK_API_KEY", "request_url": url, "reasoning_protocol": "deepseek", "thinking": "enabled", "effort": effort, "reject_redirects": true}
	if effort == "disabled" {
		extra["thinking"] = "disabled"
	}
	var p provider.Provider
	var err error
	switch protocol {
	case "chat":
		p, err = openai.New(provider.Config{Name: "official-live-chat", BaseURL: "https://api.deepseek.com", Model: model, APIKey: key, Extra: extra})
	case "anthropic":
		p, err = anthropic.New(provider.Config{Name: "official-live-anthropic", BaseURL: "https://api.deepseek.com/anthropic", Model: model, APIKey: key, Extra: extra})
	case "responses":
		p = responses.New(responses.Config{Name: "official-live-responses", BaseURL: "https://api.deepseek.com", RequestURL: url, Model: model, APIKey: key, KeyEnv: "DEEPSEEK_API_KEY", Effort: effort, Mode: "stateless", MaxOutputTokens: 2048})
	default:
		t.Fatal("unsupported test protocol")
	}
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := p.(interface{ CloseIdleConnections() }); ok {
		t.Cleanup(c.CloseIdleConnections)
	}
	return p
}

type officialRecoveryProxy struct {
	mu                            sync.Mutex
	protocol, scenario            string
	upstreamURL, sessionHeader    string
	statuses                      []int
	wireTools, wireStops          []string
	requests, upstream, mutations int
	bodies                        [][]byte
	searchResponses               [][]byte
	cancel                        context.CancelFunc
}

func (p *officialRecoveryProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read test request", 400)
		return
	}
	p.mu.Lock()
	p.requests++
	n := p.requests
	p.bodies = append(p.bodies, body)
	p.mu.Unlock()
	if n > 16 || n > 5 && p.scenario != "continuity" {
		http.Error(w, "live test request limit", 401)
		return
	}
	if p.scenario == "followup_503" && n == 2 {
		w.WriteHeader(503)
		_, _ = io.WriteString(w, `{"error":{"message":"injected temporary service failure"}}`)
		return
	}
	// Drop proof and replace the call identity only in the outbound request.
	// This forces the official service to validate unavailable replay content,
	// while the canonical local transcript still retains the completed tool.
	if p.scenario == "server_replay_rejection" && n == 2 {
		body = breakOfficialReplay(p.protocol, body)
	}
	path := map[string]string{"chat": "/chat/completions", "responses": "/responses", "anthropic": "/anthropic/v1/messages"}[p.protocol]
	target := p.upstreamURL
	if target == "" {
		target = "https://api.deepseek.com" + path
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "create upstream", 500)
		return
	}
	for _, name := range []string{"Authorization", "x-api-key", "anthropic-version", "Content-Type", "User-Agent", "x-opencode-session"} {
		if v := r.Header.Get(name); v != "" {
			req.Header.Set(name, v)
		}
	}
	if p.sessionHeader != "" {
		req.Header.Set("User-Agent", "Reasonix/live-validation")
		req.Header.Set("x-opencode-session", p.sessionHeader)
	}
	client := &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	p.mu.Lock()
	p.upstream++
	p.mu.Unlock()
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "upstream failed", 502)
		return
	}
	defer resp.Body.Close()
	p.mu.Lock()
	p.statuses = append(p.statuses, resp.StatusCode)
	p.mu.Unlock()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "upstream read failed", 502)
		return
	}
	if p.scenario == "search" {
		p.mu.Lock()
		p.searchResponses = append(p.searchResponses, bytes.Clone(data))
		p.mu.Unlock()
	}
	if p.protocol == "chat" && resp.StatusCode == 200 {
		var names, stops []string
		for _, line := range bytes.Split(data, []byte("\n")) {
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			var frame struct {
				Choices []struct {
					FinishReason string `json:"finish_reason"`
					Delta        struct {
						ToolCalls []struct {
							Function struct {
								Name string `json:"name"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))), &frame) != nil {
				continue
			}
			for _, choice := range frame.Choices {
				if choice.FinishReason != "" {
					stops = append(stops, choice.FinishReason)
				}
				for _, call := range choice.Delta.ToolCalls {
					if call.Function.Name != "" {
						names = append(names, call.Function.Name)
					}
				}
			}
		}
		p.mu.Lock()
		p.wireTools = append(p.wireTools, names...)
		p.wireStops = append(p.wireStops, stops...)
		p.mu.Unlock()
	}
	if resp.StatusCode == 200 {
		if p.scenario == "cancel_before_commit" && n == 1 {
			p.cancel()
		}
		if p.scenario == "cut_once" && n == 1 {
			data = data[:len(data)/2]
			p.mu.Lock()
			p.mutations++
			p.mu.Unlock()
		}
		if strings.HasPrefix(p.scenario, "missing") && (n == 1 || p.scenario == "missing_persistent") {
			switch p.protocol {
			case "chat":
				s := &liveReasoningStripProxy{}
				data = s.stripReasoning(data)
				p.mu.Lock()
				p.mutations += int(s.strippedFields.Load())
				p.mu.Unlock()
			case "responses":
				var count int
				data, count = stripResponsesReasoningEvents(data)
				p.mu.Lock()
				p.mutations += count
				p.mu.Unlock()
			case "anthropic":
				var count int
				data, count = stripOfficialThinking(data)
				p.mu.Lock()
				p.mutations += count
				p.mu.Unlock()
			}
		}
	}
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(data)
}
func stripOfficialThinking(data []byte) ([]byte, int) {
	var out bytes.Buffer
	count := 0
	thinking := map[int]bool{}
	for _, frame := range bytes.Split(data, []byte("\n\n")) {
		skip := false
		for _, line := range bytes.Split(frame, []byte("\n")) {
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			var e struct {
				Type  string `json:"type"`
				Index int    `json:"index"`
				Block struct {
					Type string `json:"type"`
				} `json:"content_block"`
			}
			if json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))), &e) != nil {
				continue
			}
			if e.Type == "content_block_start" && e.Block.Type == "thinking" {
				thinking[e.Index] = true
			}
			if strings.HasPrefix(e.Type, "content_block_") && thinking[e.Index] {
				skip = true
			}
		}
		if skip {
			count++
			continue
		}
		out.Write(frame)
		out.WriteString("\n\n")
	}
	return out.Bytes(), count
}

func breakOfficialReplay(protocol string, body []byte) []byte {
	var request map[string]any
	if json.Unmarshal(body, &request) != nil {
		return body
	}
	const replacement = "call_live_missing_proof"
	if protocol == "responses" {
		var input []any
		for _, v := range request["input"].([]any) {
			item := v.(map[string]any)
			if item["type"] == "reasoning" {
				continue
			}
			if item["type"] == "function_call" {
				item["id"] = "fc_live_missing_proof"
				item["call_id"] = replacement
			}
			if item["type"] == "function_call_output" {
				item["call_id"] = replacement
			}
			input = append(input, item)
		}
		request["input"] = input
	} else {
		for _, v := range request["messages"].([]any) {
			msg := v.(map[string]any)
			if protocol == "chat" {
				delete(msg, "reasoning_content")
				if calls, ok := msg["tool_calls"].([]any); ok {
					for _, c := range calls {
						c.(map[string]any)["id"] = replacement
					}
				}
				if msg["role"] == "tool" {
					msg["tool_call_id"] = replacement
				}
			} else if blocks, ok := msg["content"].([]any); ok {
				var content []any
				for _, v := range blocks {
					b := v.(map[string]any)
					if b["type"] == "thinking" {
						continue
					}
					if b["type"] == "tool_use" {
						b["id"] = replacement
					}
					if b["type"] == "tool_result" {
						b["tool_use_id"] = replacement
					}
					content = append(content, b)
				}
				msg["content"] = content
			}
		}
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return body
	}
	return encoded
}
