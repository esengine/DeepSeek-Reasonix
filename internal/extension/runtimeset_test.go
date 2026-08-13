package extension

import (
	"errors"
	"testing"
)

// countCloser records Close invocations and the order they happened in.
type countCloser struct {
	closes  *int
	name    string
	order   *[]string
	err     error
	closedN int
}

func (c *countCloser) Close() error {
	*c.closes++
	c.closedN++
	*c.order = append(*c.order, c.name)
	return c.err
}

// TestRuntimeSetCloseOnce: Close is idempotent — most closers (processes,
// sockets) are not safe to close twice, and cleanup paths overlap.
func TestRuntimeSetCloseOnce(t *testing.T) {
	closes := 0
	var order []string
	set := NewRuntimeSet(1)
	set.Add(&countCloser{closes: &closes, name: "first", order: &order})
	set.Add(&countCloser{closes: &closes, name: "second", order: &order})

	if err := set.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if closes != 2 {
		t.Fatalf("closes = %d, want 2", closes)
	}
	// Reverse registration order: later resources may depend on earlier ones.
	if len(order) != 2 || order[0] != "second" || order[1] != "first" {
		t.Fatalf("close order = %v, want [second first]", order)
	}
	if err := set.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if closes != 2 {
		t.Fatalf("closes after double Close = %d, want still 2", closes)
	}
	if !set.Closed() {
		t.Fatal("Closed() = false after Close")
	}
	if set.Len() != 0 {
		t.Fatalf("Len() = %d after Close, want 0", set.Len())
	}
}

// TestRuntimeSetCloseIfGeneration: a stale cleanup path must never close
// resources that now belong to a newer runtime.
func TestRuntimeSetCloseIfGeneration(t *testing.T) {
	closes := 0
	var order []string
	set := NewRuntimeSet(5)
	set.Add(&countCloser{closes: &closes, name: "c", order: &order})

	if set.CloseIfGeneration(4) {
		t.Fatal("CloseIfGeneration(4) closed a generation-5 set")
	}
	if closes != 0 {
		t.Fatal("wrong generation closed resources")
	}
	if !set.CloseIfGeneration(5) {
		t.Fatal("CloseIfGeneration(5) did not close the generation-5 set")
	}
	if closes != 1 {
		t.Fatalf("closes = %d, want 1", closes)
	}
	// After closing, a repeat guard call is a no-op report, not a re-close.
	set.CloseIfGeneration(5)
	if closes != 1 {
		t.Fatalf("closes after repeat = %d, want still 1", closes)
	}
}

// TestRuntimeSetAddAfterClose: registering a resource on a dead set closes it
// immediately — leaking it would strand sidecar processes.
func TestRuntimeSetAddAfterClose(t *testing.T) {
	closes := 0
	var order []string
	set := NewRuntimeSet(1)
	if err := set.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	set.Add(&countCloser{closes: &closes, name: "late", order: &order})
	if closes != 1 {
		t.Fatalf("closes = %d, want the late closer closed immediately", closes)
	}
}

// TestRuntimeSetCloseErrors: close failures are joined, not dropped, and the
// remaining closers still run.
func TestRuntimeSetCloseErrors(t *testing.T) {
	closes := 0
	var order []string
	boom := errors.New("boom")
	set := NewRuntimeSet(1)
	set.Add(&countCloser{closes: &closes, name: "ok", order: &order})
	set.Add(&countCloser{closes: &closes, name: "bad", order: &order, err: boom})
	err := set.Close()
	if !errors.Is(err, boom) {
		t.Fatalf("Close error = %v, want boom", err)
	}
	if closes != 2 {
		t.Fatalf("closes = %d, want both closers attempted", closes)
	}
}
