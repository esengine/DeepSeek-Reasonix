package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/provider"
)

// Issue #9389: a gateway resets the HTTP/2 stream after it has already streamed
// reasoning, the turn dies on "stream error: … INTERNAL_ERROR; received from
// peer", and the whole thinking pass is paid for again by hand. The reset is
// net/http's own unexported http2StreamError, which IsConnReset cannot name —
// so what answers the question is where it happened, not which error it was.
func TestStreamResetByPeerAfterOutputIsReplayable(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking hard\"}}]}\n\n")
		w.(http.Flusher).Flush()
		// Panicking mid-body is how net/http/httptest makes a server send
		// RST_STREAM(INTERNAL_ERROR) — the exact frame the report shows.
		panic(http.ErrAbortHandler)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	p, err := New(provider.Config{Name: "bailucode", BaseURL: srv.URL, Model: "m", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c, ok := p.(*client)
	if !ok {
		t.Fatalf("provider is %T, want *client", p)
	}
	c.http = srv.Client()

	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var streamErr error
	var sawReasoning bool
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkReasoning:
			sawReasoning = true
		case provider.ChunkError:
			streamErr = chunk.Err
		}
	}
	if !sawReasoning {
		t.Fatal("the fixture must stream reasoning before the reset, or it is not this bug")
	}
	if streamErr == nil {
		t.Fatal("a reset stream must report an error, not commit the turn")
	}
	var interrupted *provider.StreamInterruptedError
	if !errors.As(streamErr, &interrupted) {
		t.Fatalf("peer reset after output = %v (%T), want StreamInterruptedError so the Agent replays", streamErr, streamErr)
	}
}
