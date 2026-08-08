package extension

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type fakeBackend struct {
	id     string
	closed atomic.Bool
}

func (b *fakeBackend) ID() string { return b.id }
func (b *fakeBackend) Close(context.Context) error {
	b.closed.Store(true)
	return nil
}

func TestStableProxyReplaceDrainsPrevious(t *testing.T) {
	p := NewStableProxy()
	a := &fakeBackend{id: "a"}
	b := &fakeBackend{id: "b"}
	if err := p.Replace(context.Background(), a, 1); err != nil {
		t.Fatal(err)
	}
	if err := p.Replace(context.Background(), b, 2); err != nil {
		t.Fatal(err)
	}
	if !a.closed.Load() {
		t.Fatal("previous backend not drained")
	}
	if p.Active().ID() != "b" || p.Generation() != 2 {
		t.Fatalf("active = %v gen=%d", p.Active(), p.Generation())
	}
}

func TestStableProxyCallWithoutBackend(t *testing.T) {
	p := NewStableProxy()
	if err := p.Call(func(Backend) error { return nil }); err == nil {
		t.Fatal("expected no-backend error")
	}
}

func TestStableProxyReplaceCancelsInFlight(t *testing.T) {
	p := NewStableProxy()
	a := &fakeBackend{id: "a"}
	if err := p.Replace(context.Background(), a, 1); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- p.CallCtx(context.Background(), func(ctx context.Context, _ Backend) error {
			close(started)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		})
	}()
	<-started
	b := &fakeBackend{id: "b"}
	if err := p.Replace(context.Background(), b, 2); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected in-flight call cancelled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight call did not complete after replace")
	}
}
