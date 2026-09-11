package rpcwire

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// TestWriteStallFailsConnection covers the stdio-wedge case: the peer keeps
// the pipe open but never reads, so an unbounded write would block the caller
// forever. With MaxWriteStall set, the write aborts with WriteStallError and
// the connection fails, so later requests fail fast instead of queueing
// behind the stall.
func TestWriteStallFailsConnection(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close() // never read from pr: the pipe wedge

	conn := NewConn(pr, pw, Options{Name: "stall-test", MaxWriteStall: 50 * time.Millisecond})
	defer pw.Close()

	big := make(map[string]any)
	big["pad"] = string(make([]byte, 1<<20)) // 1 MiB, far beyond any pipe buffer

	start := time.Now()
	_, err := conn.Request(context.Background(), "never/answered", big)
	elapsed := time.Since(start)

	var stall *WriteStallError
	if !errors.As(err, &stall) {
		t.Fatalf("Request error = %v, want WriteStallError", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("stall took %s to abort, want close to 50ms", elapsed)
	}

	// The connection is terminal: the next request fails fast.
	_, err = conn.Request(context.Background(), "next/call", nil)
	if err == nil {
		t.Fatal("second request should fail on a terminal connection")
	}
	if elapsed2 := time.Since(start); elapsed2 > 5*time.Second {
		t.Fatalf("second request blocked for %s", elapsed2)
	}
}

// TestWriteCallerContextAbortLeavesConnectionAlive: a caller-side context
// deadline expiring mid-write aborts that request without killing the
// connection — a user cancel must not tear down a healthy transport — and
// every byte the reader later sees is a well-formed NDJSON frame: the
// aborted frame either never started or finished serially, never torn or
// interleaved with the next one.
func TestWriteCallerContextAbortLeavesConnectionAlive(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	conn := NewConn(pr, pw, Options{Name: "ctx-abort-test"})
	defer pw.Close()

	drained := make(chan []byte, 1)
	go func() {
		buf, _ := io.ReadAll(pr)
		drained <- buf
	}()

	big := map[string]any{"pad": string(make([]byte, 1<<20))}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := conn.Request(ctx, "never/answered", big)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Request error = %v, want context.DeadlineExceeded", err)
	}
	if err := conn.Notify("ping", map[string]string{"ok": "1"}); err != nil {
		t.Fatalf("Notify after caller-abort: %v", err)
	}
	_ = pw.Close()
	wire := <-drained

	var ping bool
	for line := range strings.SplitSeq(strings.TrimSpace(string(wire)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("torn or interleaved frame on the wire: %q... (%v)", line[:min(len(line), 80)], err)
		}
		if frame["method"] == "ping" {
			ping = true
		}
	}
	if !ping {
		t.Fatalf("ping frame missing from drained wire: %d bytes", len(wire))
	}
}

// gatedWriter blocks the first write until the test releases it, proving the
// writer goroutine is physically mid-frame before anything else happens.
type gatedWriter struct {
	started chan struct{}
	release chan struct{}
	buf     strings.Builder
}

func (g *gatedWriter) Write(b []byte) (int, error) {
	select {
	case g.started <- struct{}{}:
	default:
	}
	<-g.release
	return g.buf.Write(b)
}

// TestQueuedFrameCancelledBeforeStartNeverLands: a frame still queued behind
// a wedged writer when its caller gives up is dropped by the writer loop —
// its bytes never reach the transport.
func TestQueuedFrameCancelledBeforeStartNeverLands(t *testing.T) {
	gw := &gatedWriter{started: make(chan struct{}, 1), release: make(chan struct{})}
	conn := NewConn(strings.NewReader(""), gw, Options{Name: "queued-cancel-test"})

	// Job 1 wedges the single writer (blocked inside the gated writer).
	firstDone := make(chan error, 1)
	go func() { firstDone <- conn.Notify("first/wedged", map[string]any{"pad": strings.Repeat("x", 1<<20)}) }()
	select {
	case <-gw.started:
	case <-time.After(5 * time.Second):
		t.Fatal("writer never started the first frame")
	}

	// Job 2 queues behind it, and its caller gives up while it is still
	// queued — the writer must drop it without writing a byte.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := conn.Request(ctx, "second/queued", map[string]string{"mark": "second"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued Request error = %v, want context.DeadlineExceeded", err)
	}

	close(gw.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first frame: %v", err)
	}
	// The writer dequeues job 2 with an expired context; give it a bounded
	// moment, then inspect exactly what was written.
	wire := gw.buf.String()
	if strings.Contains(wire, "second/queued") || strings.Contains(wire, `"mark":"second"`) {
		t.Fatalf("cancelled-before-start frame reached the transport (wire=%d bytes): %.120q", len(wire), wire)
	}
	if !strings.Contains(wire, "first/wedged") {
		t.Fatal("the first frame should have completed serially once released")
	}
}

// TestNotifyAfterGracefulCloseFails (review regression): when Serve ends on a
// clean EOF, a later Notify must FAIL — the frame is gone, and reporting
// success would silently drop it. Also covers a frame enqueued in the
// close-race window: the drain loop must answer it with the terminal error.
func TestNotifyAfterGracefulCloseFails(t *testing.T) {
	serverToClientR, serverToClientW := io.Pipe()
	conn := NewConn(serverToClientR, io.Discard, Options{Name: "close-notify-test"})
	ctx := t.Context()
	serveDone := make(chan error, 1)
	go func() { serveDone <- conn.Serve(ctx) }()

	if err := conn.Notify("before/close", nil); err != nil {
		t.Fatalf("pre-close Notify: %v", err)
	}
	// Graceful transport end: peer closes both directions, Serve returns nil.
	_ = serverToClientW.Close()
	if err := <-serveDone; err != nil {
		t.Fatalf("Serve on graceful EOF: %v", err)
	}

	for i := range 20 {
		if err := conn.Notify("after/close", nil); err == nil {
			t.Fatalf("attempt %d: post-close Notify reported success for a dropped frame", i)
		}
	}

	// A request after close must fail immediately with the terminal error,
	// not hang or report nil.
	if _, err := conn.Request(context.Background(), "after/close", nil); err == nil {
		t.Fatal("post-close Request reported success")
	}
}
