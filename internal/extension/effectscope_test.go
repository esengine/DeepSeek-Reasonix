package extension

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestEffectScopeDisposeIdempotentAndReverse(t *testing.T) {
	var order []string
	s := NewEffectScope(1)
	for _, id := range []string{"a", "b", "c"} {
		if err := s.Track(Effect{
			ID: id, Class: Reversible,
			Dispose: func(context.Context) error {
				order = append(order, id)
				return nil
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Dispose(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := order; len(got) != 3 || got[0] != "c" || got[1] != "b" || got[2] != "a" {
		t.Fatalf("order = %v, want [c b a]", got)
	}
	if err := s.Dispose(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(order) != 3 {
		t.Fatalf("second dispose re-ran effects: %v", order)
	}
}

func TestEffectScopeActivationMidFailureCleanup(t *testing.T) {
	var closed atomic.Int32
	s := NewEffectScope(2)
	if err := s.Track(Effect{
		ID: "sidecar", Class: Reversible,
		Dispose: func(context.Context) error {
			closed.Add(1)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	// Simulate activation failure: dispose the partially activated scope.
	if err := s.Dispose(context.Background()); err != nil {
		t.Fatal(err)
	}
	if closed.Load() != 1 {
		t.Fatalf("closed = %d, want 1", closed.Load())
	}
}

func TestEffectScopeCleanupErrorAggregation(t *testing.T) {
	boom := errors.New("boom")
	var order []string
	s := NewEffectScope(1)
	_ = s.Track(Effect{ID: "ok", Class: Reversible, Dispose: func(context.Context) error {
		order = append(order, "ok")
		return nil
	}})
	_ = s.Track(Effect{ID: "bad", Class: Reversible, Dispose: func(context.Context) error {
		order = append(order, "bad")
		return boom
	}})
	err := s.Dispose(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if len(order) != 2 {
		t.Fatalf("order = %v, both must run", order)
	}
}

func TestEffectScopeContextCancellation(t *testing.T) {
	s := NewEffectScope(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := make(chan struct{})
	_ = s.Track(Effect{
		ID: "bg", Class: Cancelable,
		Dispose: func(c context.Context) error {
			close(started)
			select {
			case <-c.Done():
				return c.Err()
			case <-time.After(time.Second):
				return errors.New("did not observe cancel")
			}
		},
	})
	err := s.Dispose(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	select {
	case <-started:
	default:
		t.Fatal("dispose did not run")
	}
}

func TestEffectScopeIrreversibleReceipt(t *testing.T) {
	s := NewEffectScope(9)
	_ = s.Track(Effect{
		ID: "msg-send", Owner: "plugin/x", Component: "x", Class: Irreversible,
		Dispose: func(context.Context) error { return nil },
	})
	if err := s.Dispose(context.Background()); err != nil {
		t.Fatal(err)
	}
	receipts := s.Receipts()
	if len(receipts) != 1 {
		t.Fatalf("receipts = %d, want 1", len(receipts))
	}
	r := receipts[0]
	if r.Class != Irreversible || r.CompensationStatus != "not_applicable" || r.Generation != 9 {
		t.Fatalf("receipt = %+v", r)
	}
}

func TestEffectScopeTrackAfterClose(t *testing.T) {
	var closed atomic.Int32
	s := NewEffectScope(1)
	_ = s.Dispose(context.Background())
	if err := s.Track(Effect{
		ID: "late", Class: Reversible,
		Dispose: func(context.Context) error {
			closed.Add(1)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if closed.Load() != 1 {
		t.Fatal("late track must dispose immediately")
	}
}

func TestEffectScopeDuplicateID(t *testing.T) {
	s := NewEffectScope(1)
	_ = s.Track(Effect{ID: "x", Class: Reversible, Dispose: func(context.Context) error { return nil }})
	if err := s.Track(Effect{ID: "x", Class: Reversible, Dispose: func(context.Context) error { return nil }}); err == nil {
		t.Fatal("duplicate id accepted")
	}
}
