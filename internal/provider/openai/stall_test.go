package openai

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

// TestStreamStallTimesOut covers issue #3374: a half-open connection (a proxy
// switched mid-stream) sends the SSE head then goes silent without an RST, so
// scanner.Scan() would block forever and Ctrl+C-less sessions hang until kill -9.
// The idle watchdog must surface a stall error instead of hanging.
func TestStreamStallTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flush(w)
		_, _ = io.WriteString(w, ": keep-alive\n\n") // one comment, resets the watchdog once
		flush(w)
		<-release // then stall: never send data, never close — half-open connection
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

func TestReadStreamSendUnblocksOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	resp := &http.Response{Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))}
	out := make(chan provider.Chunk)
	done := make(chan struct{})

	go func() {
		_, _ = (&client{name: "openai"}).readStream(ctx, resp, out)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("readStream remained blocked sending to an abandoned reader after context cancellation")
	}
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
