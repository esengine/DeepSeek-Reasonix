package plugin

import (
	"context"
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

func TestStdioCallRespectsExistingDeadline(t *testing.T) {
	tr := &stdioTransport{
		name:    "server",
		stdin:   discardWriteCloser{},
		pending: map[int]chan rpcResponse{},
	}

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
	case <-time.After(1 * time.Second):
		t.Fatal("stdio call did not return within caller deadline")
	}
}

func TestStdioCallCancelReturnsContextCanceled(t *testing.T) {
	tr := &stdioTransport{
		name:    "slow-server",
		stdin:   discardWriteCloser{},
		pending: map[int]chan rpcResponse{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := tr.call(ctx, "tools/call", map[string]any{})
		done <- err
	}()

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
		t.Fatal("stdio call did not return within 2s of cancel")
	}
}
