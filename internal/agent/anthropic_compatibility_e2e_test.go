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
)

func TestCustomAnthropicCompatibilityToolLoop(t *testing.T) {
	for _, tc := range []struct {
		name, first    string
		rejectFollowup bool
	}{
		{"complete missing thinking", missingReasoningToolSSE, false},
		{"complete unsigned thinking", recoveredReasoningToolSSE, false},
		{"server rejects unsigned history", recoveredReasoningToolSSE, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var bodies [][]byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				mu.Lock()
				bodies = append(bodies, body)
				n := len(bodies)
				mu.Unlock()
				if n == 2 && tc.rejectFollowup {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"error":{"message":"The content[].thinking in the thinking mode must be passed back to the API"}}`)
					return
				}
				if n > 3 {
					t.Errorf("unexpected HTTP attempt %d", n)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				response := finalAnswerSSE
				if n == 1 {
					response = tc.first
				}
				_, _ = io.WriteString(w, response)
			}))
			defer srv.Close()
			p, err := anthropic.New(provider.Config{Name: "custom-anthropic", BaseURL: srv.URL, Model: "deepseek-v4-flash", APIKey: "fake-key", Extra: map[string]any{"thinking": "adaptive"}})
			if err != nil {
				t.Fatal(err)
			}
			sink := &recordSink{}
			a := New(p, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: t.TempDir()}, sink)
			if err := a.Run(withNoClosedLoop(context.Background()), "go"); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			expected := 2
			if tc.rejectFollowup {
				expected = 3
			}
			if len(bodies) != expected || len(sink.kinds(event.ToolResult)) != 1 {
				t.Fatalf("HTTP=%d tools=%d", len(bodies), len(sink.kinds(event.ToolResult)))
			}
			if !bytes.Contains(bodies[1], []byte(`"type":"tool_result"`)) {
				t.Fatalf("follow-up omitted completed tool: %s", bodies[1])
			}
			if tc.first == recoveredReasoningToolSSE && !bytes.Contains(bodies[1], []byte(`"thinking":"call echo safely"`)) {
				t.Fatalf("lost unsigned thinking: %s", bodies[1])
			}
			if bytes.Contains(bodies[1], []byte(`"signature"`)) {
				t.Fatal("fabricated a signature")
			}
			if tc.rejectFollowup {
				if !bytes.Contains(bodies[2], []byte("completed_tools")) || bytes.Contains(bodies[2], []byte(`"type":"tool_use"`)) {
					t.Fatalf("invalid recovery view: %s", bodies[2])
				}
			} else if len(sink.kinds(event.Retrying)) != 0 {
				t.Fatal("compatible turn regenerated")
			}
			var originals int
			for _, m := range a.Session().Snapshot() {
				if m.Role == provider.RoleAssistant && m.ReasoningContent == "call echo safely" {
					originals++
				}
			}
			if tc.first == recoveredReasoningToolSSE && originals != 1 {
				t.Fatal("canonical thinking not retained")
			}
		})
	}
}

func TestNativeTextConversionDoesNotClaimMissingReasoningIncident(t *testing.T) {
	p, err := anthropic.New(provider.Config{Name: "native", Model: "claude-sonnet-4-6", APIKey: "fake-key", Extra: map[string]any{"thinking": "adaptive"}})
	if err != nil {
		t.Fatal(err)
	}
	a := New(p, echoRegistry(), NewSession(""), Options{MissingReasoningWarnStateDir: t.TempDir()}, event.Discard)
	m := provider.Message{Role: provider.RoleAssistant, ReasoningContent: "complete unsigned text", ReasoningState: provider.ReasoningComplete}
	if missing, retry := a.observeMissingAssistantReasoning(m, true); missing || retry || a.sess.missingReasoning.active {
		t.Fatal("compatible text consumed strict recovery budget")
	}
}
