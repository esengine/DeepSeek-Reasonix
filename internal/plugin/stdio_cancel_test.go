package plugin

import (
	"context"
	"strings"
	"testing"
	"time"
)

type discardWriteCloser struct{}

func (discardWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriteCloser) Close() error                { return nil }

// TestStdioCallReturnsOnContextCancel pins that a stdio call unblocks when its
// context is cancelled even though the server never replies. The stdio child is
// bound to the session, not the turn, so without this a hung server would hang a
// cancelled turn forever. No reader goroutine runs here, so the reply never
// arrives — only ctx cancellation can return the call.
func TestStdioCallReturnsOnContextCancel(t *testing.T) {
	tr := &stdioTransport{
		name:    "hung",
		stdin:   discardWriteCloser{},
		pending: map[int]chan rpcResponse{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := tr.call(ctx, "tools/call", map[string]any{})
		done <- err
	}()

	time.Sleep(100 * time.Millisecond) // let the call park in its select
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled call returned nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stdio call did not return within 2s of ctx cancel — a hung server hangs the turn")
	}
}

// TestStdioCallTimesOutWithoutDeadline proves that a stdio call returns after
// the per-call timeout when the caller's context has no deadline. This is the
// regression test for #4299: without the timeout, a slow MCP server blocks the
// agent's turn indefinitely.
func TestStdioCallTimesOutWithoutDeadline(t *testing.T) {
	tr := &stdioTransport{
		name:        "slow-server",
		stdin:       discardWriteCloser{},
		pending:     map[int]chan rpcResponse{},
		callTimeout: 100 * time.Millisecond, // short timeout for test
	}

	// Context with NO deadline — only cancellation (like the agent's turn ctx).
	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		_, err := tr.call(ctx, "tools/call", map[string]any{})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("timed-out call returned nil error")
		}
		if !strings.Contains(err.Error(), "context deadline exceeded") {
			t.Fatalf("expected deadline exceeded error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stdio call did not return within 2s — timeout not working")
	}
}

// TestStdioCallRespectsExistingDeadline proves that when the caller's context
// already has a deadline, the transport does NOT override it with its own
// timeout. The caller's deadline takes precedence.
func TestStdioCallRespectsExistingDeadline(t *testing.T) {
	tr := &stdioTransport{
		name:        "server",
		stdin:       discardWriteCloser{},
		pending:     map[int]chan rpcResponse{},
		callTimeout: 10 * time.Second, // long call timeout
	}

	// Context with a SHORT deadline — should be respected, not overridden.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := tr.call(ctx, "tools/call", map[string]any{})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("timed-out call returned nil error")
		}
		// Should fire at ~100ms (caller's deadline), not 10s (callTimeout).
	case <-time.After(1 * time.Second):
		t.Fatal("stdio call did not return within 1s — caller deadline not respected")
	}
}

// TestStdioCallCancelOverridesTimeout proves that if the user cancels the turn
// while we are waiting for a timed-out server, the call returns immediately
// with context.Canceled instead of context.DeadlineExceeded.
func TestStdioCallCancelOverridesTimeout(t *testing.T) {
	tr := &stdioTransport{
		name:        "slow-server",
		stdin:       discardWriteCloser{},
		pending:     map[int]chan rpcResponse{},
		callTimeout: 500 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := tr.call(ctx, "tools/call", map[string]any{})
		done <- err
	}()

	// Cancel before the timeout fires — should return context.Canceled,
	// not context.DeadlineExceeded.
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled call returned nil error")
		}
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stdio call did not return within 2s of cancel during timeout")
	}
}
