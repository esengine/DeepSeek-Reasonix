package openai

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// rstAfter writes a 200 SSE head plus the given prelude, then forces a TCP RST
// (SetLinger(0) + Close) so the client read fails like a proxy that idle-drops
// the long-lived connection (wsarecv: forcibly closed), not a clean EOF.
func rstAfter(t *testing.T, w http.ResponseWriter, prelude string) {
	t.Helper()
	hj, ok := w.(http.Hijacker)
	if !ok {
		t.Fatal("ResponseWriter is not a Hijacker")
	}
	conn, buf, err := hj.Hijack()
	if err != nil {
		t.Fatalf("hijack: %v", err)
	}
	_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\n\r\n")
	_, _ = buf.WriteString(prelude)
	_ = buf.Flush()
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetLinger(0)
	}
	_ = conn.Close()
}

// TestStreamReconnectsOnEarlyConnReset reproduces issue #3148: a local proxy
// (v2rayN/sing-box) forcibly closes the idle SSE connection during a reasoner's
// first-token gap, before any token is emitted. The drop must be replayed
// transparently — the caller sees one clean stream, never an error.
func TestStreamReconnectsOnEarlyConnReset(t *testing.T) {
	var reqs int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs++
		if reqs == 1 {
			rstAfter(t, w, ": keep-alive\n\n") // a comment line, zero model output
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"recovered\"}}]}\n\n")
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
			t.Fatalf("early conn reset should be replayed, not surfaced: %v", chunk.Err)
		}
		if chunk.Type == provider.ChunkText {
			text.WriteString(chunk.Text)
		}
	}
	if text.String() != "recovered" {
		t.Errorf("text = %q, want %q", text.String(), "recovered")
	}
	if reqs != 2 {
		t.Errorf("server saw %d requests, want 2 (one reset + one replay)", reqs)
	}
}

// TestStreamDoesNotReplayAfterOutput guards against duplicated output: once a
// token has streamed, a mid-stream reset must surface as an error rather than
// replaying the request (which would re-emit the already-shown text).
func TestStreamDoesNotReplayAfterOutput(t *testing.T) {
	var reqs int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs++
		rstAfter(t, w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
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
	var gotErr bool
	var gotInterrupted bool
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkError:
			gotErr = true
			var interrupted *provider.StreamInterruptedError
			gotInterrupted = errors.As(chunk.Err, &interrupted)
		}
	}
	if text.String() != "partial" {
		t.Errorf("text = %q, want %q (the one delta that streamed)", text.String(), "partial")
	}
	if !gotErr {
		t.Error("a reset after output should surface a ChunkError")
	}
	if !gotInterrupted {
		t.Error("a reset after output should be marked as a stream interruption")
	}
	if reqs != 1 {
		t.Errorf("server saw %d requests, want 1 (no replay after output)", reqs)
	}
}
