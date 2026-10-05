package extension

import (
	"context"
	"testing"
	"time"
)

func TestHostStreamRegistryCancelGeneration(t *testing.T) {
	r := NewHostStreamRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	untrack := r.Track(5, cancel)
	if r.Count(5) != 1 {
		t.Fatalf("count = %d", r.Count(5))
	}
	r.CancelGeneration(5)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel not fired")
	}
	if r.Count(5) != 0 {
		t.Fatal("expected cleared")
	}
	untrack() // idempotent
}

func TestHostStreamUntrack(t *testing.T) {
	r := NewHostStreamRegistry()
	_, cancel := context.WithCancel(context.Background())
	untrack := r.Track(3, cancel)
	untrack()
	if r.Count(3) != 0 {
		t.Fatal("untrack should remove")
	}
	r.CancelGeneration(3) // no-op
}

func TestHostStreamDrainHook(t *testing.T) {
	g := NewPublishGate().WithDrainTTL(time.Millisecond)
	r := NewHostStreamRegistry(g)
	ctx, cancel := context.WithCancel(context.Background())
	_ = r.Track(42, cancel)
	// Simulate drain timeout path.
	g.FireDrainCancels(42)
	select {
	case <-ctx.Done():
	default:
		t.Fatal("FireDrainCancels should cancel host stream")
	}
}

func TestHostStreamTrackAfterExpiredGenerationCancelsImmediately(t *testing.T) {
	g := NewPublishGate()
	g.Publish(41)
	g.Publish(42)
	g.ForceExpireDrain(41)
	r := NewHostStreamRegistry(g)
	ctx, cancel := context.WithCancel(context.Background())
	_ = r.Track(41, cancel)
	select {
	case <-ctx.Done():
	default:
		t.Fatal("stream registered after generation expiry must cancel immediately")
	}
	if got := r.Count(41); got != 0 {
		t.Fatalf("expired generation retained %d streams", got)
	}
}
