package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
	"reasonix/internal/tool"
)

type recordSink struct {
	mu  sync.Mutex
	evs []event.Event
}

func (s *recordSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evs = append(s.evs, e)
}

func (s *recordSink) kinds(k event.Kind) []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []event.Event
	for _, e := range s.evs {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

// TestAgentEmitsRetryingThenStreams drives the whole chain end-to-end: a real
// OpenAI-compatible provider hits an httptest server that returns 503 twice then
// a valid SSE stream. The agent must emit a Retrying event per backoff (so the
// composer can show "retrying n/m") and still deliver the streamed answer.
func TestAgentEmitsRetryingThenStreams(t *testing.T) {
	var reqs int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs++
		if reqs <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"overloaded"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi there\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	prov, err := openai.New(provider.Config{Name: "deepseek", BaseURL: srv.URL, Model: "deepseek-v4", APIKey: "k"})
	if err != nil {
		t.Fatalf("New provider: %v", err)
	}

	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, sink)
	if err := a.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	retries := sink.kinds(event.Retrying)
	if len(retries) != 2 || retries[0].RetryAttempt != 1 || retries[1].RetryAttempt != 2 {
		t.Fatalf("want two Retrying events (1,2), got %+v", retries)
	}
	if retries[0].RetryMax != provider.MaxRetries {
		t.Errorf("RetryMax = %d, want %d", retries[0].RetryMax, provider.MaxRetries)
	}

	var answer strings.Builder
	for _, e := range sink.kinds(event.Text) {
		answer.WriteString(e.Text)
	}
	if !strings.Contains(answer.String(), "hi there") {
		t.Errorf("streamed answer = %q, want it to contain %q", answer.String(), "hi there")
	}
}
