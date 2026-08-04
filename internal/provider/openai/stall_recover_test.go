package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/provider"
)

// TestStreamStallRecoversViaReconnect exercises the watchdog's primary target:
// a proxy that drops a long-lived SSE connection silently mid-stream (no FIN,
// no RST) during a reasoner's first-token gap, before any model output. The
// stall must be treated as recoverable and replayed — like a clean idle-close —
// not surfaced as a hard failure. Before the fix the stall error was plain and
// bypassed streamWithReconnect, so this test would fail with a "stalled" error.
func TestStreamStallRecoversViaReconnect(t *testing.T) {
	release := make(chan struct{})
	var reqs atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if reqs.Add(1) == 1 {
			// First connection: one keep-alive comment (resets the watchdog
			// once) then total silence — a half-open connection that never
			// delivers data and never closes.
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flush(w)
			_, _ = io.WriteString(w, ": keep-alive\n\n")
			flush(w)
			<-release
			return
		}
		// Reconnect serves a complete stream.
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"recovered\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	defer close(release)

	p, err := New(provider.Config{Name: "deepseek", BaseURL: srv.URL, Model: "deepseek-v4", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.(*client).idleTimeout = 150 * time.Millisecond

	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text strings.Builder
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("a pre-output half-open stall should reconnect, not surface: %v", chunk.Err)
		}
		if chunk.Type == provider.ChunkText {
			text.WriteString(chunk.Text)
		}
	}
	if got := text.String(); got != "recovered" {
		t.Errorf("text = %q, want %q (stall should have triggered a reconnect)", got, "recovered")
	}
	if got := reqs.Load(); got != 2 {
		t.Errorf("server saw %d requests, want 2 (one stall + one replay)", got)
	}
}

// TestStreamToleratesEmptyDataLine: an OpenAI-compatible gateway may emit an
// empty or heartbeat `data:` line. Before the fix the empty payload failed
// json.Unmarshal ("unexpected end of JSON input") and fatally aborted the whole
// turn; the sibling Anthropic/Responses transports tolerate it.
func TestStreamToleratesEmptyDataLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data:\n\n") // heartbeat / empty payload
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p, err := New(provider.Config{Name: "deepseek", BaseURL: srv.URL, Model: "deepseek-v4", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text strings.Builder
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			t.Fatalf("empty data: line should be tolerated, not abort the stream: %v", chunk.Err)
		}
		if chunk.Type == provider.ChunkText {
			text.WriteString(chunk.Text)
		}
	}
	if got := text.String(); got != "ok" {
		t.Errorf("text = %q, want %q", got, "ok")
	}
}
