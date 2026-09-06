package agent

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/anthropic"
	"reasonix/internal/provider/openai"
	"reasonix/internal/provider/responses"
)

func TestTruncatedJSONStreamRecoversWithoutExecutingPartialTools(t *testing.T) {
	for _, protocol := range []string{"chat", "anthropic"} {
		for _, cut := range []bool{true, false} {
			name := protocol + "/malformed_line"
			if cut {
				name = protocol + "/unterminated_fragment"
			}
			t.Run(name, func(t *testing.T) {
				var mu sync.Mutex
				var bodies [][]byte
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					mu.Lock()
					bodies = append(bodies, body)
					n := len(bodies)
					mu.Unlock()
					w.Header().Set("Content-Type", "text/event-stream")
					if n == 1 {
						// Complete tool arguments received before EOF still must not execute.
						first := `data: {"choices":[{"delta":{"reasoning_content":"partial","tool_calls":[{"index":0,"id":"uncommitted","type":"function","function":{"name":"echo","arguments":"{\"text\":\"unsafe\"}"}}]}}]}` + "\n\n"
						if protocol == "anthropic" {
							first = `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"uncommitted","name":"echo","input":{"text":"unsafe"}}}` + "\n\n"
						}
						_, _ = io.WriteString(w, first+`data: {"delta":nu`)
						if !cut {
							_, _ = io.WriteString(w, "\n\n")
						}
						return
					}
					if n > 2 {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					reply := `data: {"choices":[{"delta":{"content":"recovered"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":2,"total_tokens":14}}` + "\n\ndata: [DONE]\n\n"
					if protocol == "anthropic" {
						reply = finalAnswerSSE
					}
					_, _ = io.WriteString(w, reply)
				}))
				defer srv.Close()
				cfg := provider.Config{Name: "fragment", BaseURL: srv.URL, APIKey: "fixture", Model: "deepseek-v4-flash", Extra: map[string]any{"reasoning_protocol": "deepseek", "thinking": "enabled"}}
				var p provider.Provider
				var err error
				if protocol == "chat" {
					p, err = openai.New(cfg)
				} else {
					p, err = anthropic.New(cfg)
				}
				if err != nil {
					t.Fatal(err)
				}
				sink := &recordSink{}
				a := New(p, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: t.TempDir()}, sink)
				err = a.Run(withNoClosedLoop(context.Background()), "go")
				if (err == nil) != cut {
					t.Fatalf("cut=%v error=%v", cut, err)
				}
				if len(sink.kinds(event.ToolResult)) != 0 {
					t.Fatal("uncommitted tool executed")
				}
				mu.Lock()
				defer mu.Unlock()
				if !cut {
					if len(bodies) != 1 {
						t.Fatal("malformed complete event retried")
					}
					return
				}
				if len(bodies) != 2 || !bytes.Equal(bodies[0], bodies[1]) {
					t.Fatal("retry lost frozen request")
				}
				usages := sink.kinds(event.Usage)
				if len(usages) != 1 || usages[0].Usage == nil || !usages[0].Usage.Unknown || usages[0].Usage.RequestCount != 2 {
					t.Fatalf("usage=%+v", usages)
				}
			})
		}
	}
}

func TestResponsesPassbackRejectionRepairsHistoryWithoutRepeatingTool(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		n := len(bodies)
		mu.Unlock()
		if n == 2 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "{\"error\":{\"message\":\"The `reasoning_text` in the thinking mode must be passed back to the API.\",\"type\":\"invalid_request_error\"}}")
			return
		}
		if n > 4 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = io.WriteString(w, responsesToolWithoutReasoningSSE)
		} else {
			_, _ = io.WriteString(w, responsesFinalAnswerSSE)
		}
	}))
	defer srv.Close()
	p := responses.New(responses.Config{Name: "responses", BaseURL: "https://api.deepseek.com", RequestURL: srv.URL, Model: "deepseek-v4-flash", APIKey: "fixture", Effort: "high", Mode: "stateless"})
	sink := &recordSink{}
	a := New(p, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: t.TempDir()}, sink)
	if err := a.Run(withNoClosedLoop(context.Background()), "go"); err != nil {
		t.Fatal(err)
	}
	if err := a.Run(withNoClosedLoop(context.Background()), "A new question: acknowledge the previous result without calling tools."); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 4 || len(sink.kinds(event.ToolResult)) != 1 {
		t.Fatalf("requests=%d executions=%d", len(bodies), len(sink.kinds(event.ToolResult)))
	}
	if bytes.Contains(bodies[3], []byte(`"type":"function_call"`)) || !bytes.Contains(bodies[3], []byte("echoed: hi")) {
		t.Fatal("later turn restored invalid tool history or lost completed results")
	}
	if !bytes.Contains(bodies[2], []byte("echoed: hi")) {
		t.Fatal("repair dropped the actual completed tool output")
	}
	if !bytes.Contains(bodies[2], []byte("completed_tools")) || bytes.Contains(bodies[2], []byte(`"type":"function_call"`)) {
		t.Fatal("repair lost completed facts or retained invalid call")
	}
}
