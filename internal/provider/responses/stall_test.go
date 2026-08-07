package responses

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/provider"
)

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// startHTTP2TLSServer returns an httptest TLS server with HTTP/2 enabled and a
// client that trusts its certificate. The handler must keep the connection
// open long enough for the client to negotiate h2 (verified via sawHTTP2).
func startHTTP2TLSServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *http.Client, *atomic.Bool) {
	t.Helper()
	var sawHTTP2 atomic.Bool
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 {
			sawHTTP2.Store(true)
		}
		handler(w, r)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	client := srv.Client()
	// Force HTTP/2 negotiation on the TLS transport (httptest enables h2 on the
	// server; the client must also advertise it).
	if tr, ok := client.Transport.(*http.Transport); ok {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test-only self-signed cert
		if err := http2ConfigureTransport(tr); err != nil {
			t.Fatalf("configure HTTP/2 transport: %v", err)
		}
	}
	return srv, client, &sawHTTP2
}

// http2ConfigureTransport enables HTTP/2 on tr without importing golang.org/x/net/http2
// when the stdlib transport already supports it via ForceAttemptHTTP2.
func http2ConfigureTransport(tr *http.Transport) error {
	tr.ForceAttemptHTTP2 = true
	return nil
}

// TestStreamStallTimesOutHTTP2 covers a half-open HTTP/2 body (headers received,
// then silence without RST). The idle watchdog must close the body and surface
// an idle_timeout StreamInterrupt so the Controller can emit a single TurnDone
// (#7811, HTTP/2 path).
func TestStreamStallTimesOutHTTP2(t *testing.T) {
	release := make(chan struct{})
	srv, httpClient, sawHTTP2 := startHTTP2TLSServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flush(w)
		// One comment keeps the connection "alive" once, then stall forever.
		_, _ = io.WriteString(w, ": keep-alive\n\n")
		flush(w)
		<-release
	})
	defer close(release)

	p := New(Config{Name: "responses", BaseURL: srv.URL, Model: "model", APIKey: "k"}).(*client)
	p.http = httpClient
	p.idleTimeout = 150 * time.Millisecond

	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
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
				if !sawHTTP2.Load() {
					t.Fatal("stall path did not run over HTTP/2 (ProtoMajor != 2)")
				}
				if !strings.Contains(chunk.Err.Error(), "idle timeout") {
					t.Fatalf("error = %v, want idle timeout", chunk.Err)
				}
				if provider.StreamInterruptReason(chunk.Err) != provider.StreamInterruptIdleTimeout {
					t.Fatalf("reason = %q, want %q", provider.StreamInterruptReason(chunk.Err), provider.StreamInterruptIdleTimeout)
				}
				return
			}
		case <-deadline:
			t.Fatal("stream did not time out on a stalled HTTP/2 body")
		}
	}
}

// TestMissingTerminalEventSurfacesPrematureEOF ensures connection close before
// response.completed/failed/incomplete is not treated as a successful turn.
func TestMissingTerminalEventSurfacesPrematureEOF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Partial text delta, then close the body without a terminal event.
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n")
		flush(w)
	}))
	defer srv.Close()

	p := New(Config{Name: "responses", BaseURL: srv.URL, Model: "model", APIKey: "k"}).(*client)
	p.idleTimeout = time.Second

	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var sawError bool
	for chunk := range ch {
		if chunk.Type == provider.ChunkError {
			sawError = true
			if provider.StreamInterruptReason(chunk.Err) != provider.StreamInterruptPrematureEOF {
				t.Fatalf("reason = %q, want %q (err=%v)",
					provider.StreamInterruptReason(chunk.Err), provider.StreamInterruptPrematureEOF, chunk.Err)
			}
		}
	}
	if !sawError {
		t.Fatal("expected premature-EOF error when terminal event is missing")
	}
}

// TestSendChunkUnblocksOnContextCancel covers the path where the consumer stops
// reading and the stream must not hang forever inside sendChunk. Timing uses a
// blocking-hook channel — no fixed sleep.
func TestSendChunkUnblocksOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// Unbuffered channel so sendChunk blocks on the second select branch.
	out := make(chan provider.Chunk)
	done := make(chan struct{})
	enteredBlocking := make(chan struct{})

	prev := sendChunkEnterBlocking
	sendChunkEnterBlocking = func() { close(enteredBlocking) }
	t.Cleanup(func() { sendChunkEnterBlocking = prev })

	go func() {
		ok := sendChunk(ctx, out, provider.Chunk{Type: provider.ChunkText, Text: "blocked"})
		if ok {
			t.Error("sendChunk returned true after cancel")
		}
		close(done)
	}()

	select {
	case <-enteredBlocking:
	case <-time.After(2 * time.Second):
		t.Fatal("sendChunk never entered the blocking select")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sendChunk remained blocked after context cancellation")
	}
}

// TestReadStreamContextCancelClosesBody ensures Cancel mid-stream unblocks the
// scanner and closes the response body without leaking the goroutine. Timing is
// driven by the first delivered chunk — no fixed sleep.
func TestReadStreamContextCancelClosesBody(t *testing.T) {
	var bodyClosed sync.WaitGroup
	bodyClosed.Add(1)
	pr, pw := io.Pipe()
	resp := &http.Response{Body: &closeNotifyBody{ReadCloser: pr, onClose: bodyClosed.Done}}

	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan provider.Chunk, 4)
	done := make(chan struct{})
	go func() {
		(&client{idleTimeout: time.Minute}).readStream(ctx, resp, out, nil)
		close(done)
	}()

	// Write a non-terminal event and wait until it is delivered — proves the
	// scanner is live before we cancel.
	_, _ = io.WriteString(pw, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"x\"}\n\n")
	select {
	case chunk := <-out:
		if chunk.Type != provider.ChunkText || chunk.Text != "x" {
			t.Fatalf("first chunk = %+v, want text delta x", chunk)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first stream chunk before cancel")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readStream did not exit after context cancel")
	}

	closed := make(chan struct{})
	go func() {
		bodyClosed.Wait()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("response body was not closed after context cancel")
	}
	_ = pw.Close()
}

type closeNotifyBody struct {
	io.ReadCloser
	onClose func()
	once    sync.Once
}

func (b *closeNotifyBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.onClose)
	return err
}
