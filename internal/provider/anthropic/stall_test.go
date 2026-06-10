package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
)

// TestStreamStallTimesOut covers issue #3374 for the Anthropic provider: a
// half-open connection sends the SSE head then goes silent without an RST, which
// would hang scanner.Scan() forever. The idle watchdog must surface a stall error.
func TestStreamStallTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = io.WriteString(w, ": ping\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-release // stall: never send data, never close
	}))
	defer srv.Close()
	defer close(release)

	p, err := New(provider.Config{Name: "claude", BaseURL: srv.URL, Model: "claude-opus-4-8", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.(*client).idleTimeout = 150 * time.Millisecond
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				t.Fatal("stream closed without surfacing a stall error")
			}
			if chunk.Type == provider.ChunkError {
				if !strings.Contains(chunk.Err.Error(), "stalled") {
					t.Fatalf("error = %v, want a 'stalled' error", chunk.Err)
				}
				return
			}
		case <-deadline:
			t.Fatal("stream did not time out on a stalled connection — it hung")
		}
	}
}
