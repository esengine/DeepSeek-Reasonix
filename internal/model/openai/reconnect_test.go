package openai

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
)

// resetAfter answers every request with a 200 SSE head whose body yields the
// prelude and then fails the way a peer reset does. A real socket RST cannot
// pin the phase: whether net/http sees it before or after the headers is up to
// the OS scheduler, and a header-phase reset is retried by design (#8327).
func resetAfter(p provider.Provider, prelude string) *atomic.Int32 {
	var reqs atomic.Int32
	p.(*client).http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		reqs.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       &cutBody{r: strings.NewReader(prelude)},
			Request:    r,
		}, nil
	})}
	return &reqs
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type cutBody struct{ r io.Reader }

func (b *cutBody) Read(p []byte) (int, error) {
	if n, _ := b.r.Read(p); n > 0 {
		return n, nil
	}
	return 0, &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
}

func (b *cutBody) Close() error { return nil }

// TestStreamSurfacesEarlyConnResetAsInterrupt moves body-phase replay to the
// Agent: a pre-output connection reset is StreamInterruptedError, not an
// in-provider transparent reconnect (avoids stacked retry budgets).
func TestStreamSurfacesEarlyConnResetAsInterrupt(t *testing.T) {
	p, err := New(provider.Config{Name: "deepseek", BaseURL: "http://gateway.invalid", Model: "deepseek-v4", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	reqs := resetAfter(p, ": keep-alive\n\n") // a comment line, zero model output
	ch, err := p.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var gotInterrupted bool
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			var interrupted *provider.StreamInterruptedError
			gotInterrupted = errors.As(chunk.Err, &interrupted)
		}
	}
	if !gotInterrupted {
		t.Error("early conn reset must surface as StreamInterruptedError for Agent replay")
	}
	if n := reqs.Load(); n != 1 {
		t.Errorf("server saw %d requests, want 1 (no provider body replay)", n)
	}
}

func TestStreamCancelDoesNotReconnect(t *testing.T) {
	var reqs atomic.Int32
	ready := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first := reqs.Add(1) == 1
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": keep-alive\n\n")
		flush(w)
		if first {
			close(ready)
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer func() {
		// The request context can outlive the stream channel on Windows. This
		// test owns only the no-replay contract, so release its fixture handler
		// before waiting for server shutdown.
		close(release)
		srv.Close()
	}()

	p, err := New(provider.Config{Name: "deepseek", BaseURL: srv.URL, Model: "deepseek-v4", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive the streaming request")
	}
	cancel()

	var got error
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			got = chunk.Err
		}
	}
	// Depending on whether the server close or the client watchdog observes
	// cancellation first, the stream may close silently or surface cancellation.
	// The contract guarded here is that cancellation never triggers a replay.
	if got != nil && !errors.Is(got, context.Canceled) {
		t.Fatalf("stream error = %v, want nil or context.Canceled", got)
	}
	if reqs.Load() != 1 {
		t.Fatalf("cancelled stream reconnected; server saw %d requests, want 1", reqs.Load())
	}
}

// TestStreamTreatsCleanEOFWithoutDoneAsCut reproduces issue #3953: a proxy that
// idle-closes the SSE connection with a clean FIN ends the scan with no error,
// which used to commit the turn as complete. Body-phase cuts surface as
// StreamInterruptedError so the Agent can replay the frozen request.
func TestStreamTreatsCleanEOFWithoutDoneAsCut(t *testing.T) {
	var reqs int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": keep-alive\n\n") // clean close, no [DONE], no finish_reason
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

	var gotInterrupted bool
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkToolCall:
			t.Fatalf("incomplete stream must not emit tool calls: %+v", chunk.ToolCall)
		case provider.ChunkError:
			var interrupted *provider.StreamInterruptedError
			gotInterrupted = errors.As(chunk.Err, &interrupted)
		}
	}
	if !gotInterrupted {
		t.Error("clean EOF before terminal must surface as StreamInterruptedError")
	}
	if reqs != 1 {
		t.Errorf("server saw %d requests, want 1 (no provider body replay)", reqs)
	}
}

// TestStreamDropsPartialToolCallOnCleanEOF is the post-output half of #3953: the
// connection dies mid-tool-call after the call's start was forwarded. The partial
// arguments must never surface as a ChunkToolCall; the cut surfaces as a stream
// interruption so the agent's recovery path takes over.
func TestStreamDropsPartialToolCallOnCleanEOF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"bash\",\"arguments\":\"{\"}}]}}]}\n\n")
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

	var gotInterrupted bool
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkToolCall:
			t.Fatalf("partial tool call surfaced: %+v", chunk.ToolCall)
		case provider.ChunkError:
			var interrupted *provider.StreamInterruptedError
			gotInterrupted = errors.As(chunk.Err, &interrupted)
		}
	}
	if !gotInterrupted {
		t.Error("a cut after the tool-call start should surface as a stream interruption")
	}
}

// TestStreamAcceptsFinishReasonWithoutDone keeps gateways that omit the [DONE]
// sentinel working: a finish_reason marks the turn complete on its own.
func TestStreamAcceptsFinishReasonWithoutDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\n")
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
			t.Fatalf("finish_reason without [DONE] should complete cleanly: %v", chunk.Err)
		}
		if chunk.Type == provider.ChunkText {
			text.WriteString(chunk.Text)
		}
	}
	if text.String() != "hello" {
		t.Errorf("text = %q, want %q", text.String(), "hello")
	}
}

// TestStreamDoesNotReplayAfterOutput guards against duplicated output: once a
// token has streamed, a mid-stream reset must surface as an error rather than
// replaying the request (which would re-emit the already-shown text).
func TestStreamDoesNotReplayAfterOutput(t *testing.T) {
	p, err := New(provider.Config{Name: "deepseek", BaseURL: "http://gateway.invalid", Model: "deepseek-v4", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	reqs := resetAfter(p, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
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
	if n := reqs.Load(); n != 1 {
		t.Errorf("server saw %d requests, want 1 (no replay after output)", n)
	}
}
