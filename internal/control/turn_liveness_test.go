package control

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/event"
)

// A running turn that produces no events for the stall threshold gets exactly
// one warning per silent stretch; any progress re-arms it.
func TestStalledTurnWarnsOncePerSilence(t *testing.T) {
	oldInterval, oldThreshold := midTurnSnapshotInterval.Load(), turnStallThreshold.Load()
	midTurnSnapshotInterval.Store(int64(10 * time.Millisecond))
	turnStallThreshold.Store(int64(80 * time.Millisecond))
	t.Cleanup(func() {
		midTurnSnapshotInterval.Store(oldInterval)
		turnStallThreshold.Store(oldThreshold)
	})

	notices := make(chan event.Event, 8)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Code == event.NoticeCodeTurnStalled {
			notices <- e
		}
	})})
	t.Cleanup(c.Close)

	started := make(chan struct{})
	c.runGuarded(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started

	select {
	case n := <-notices:
		if n.Level != event.LevelWarn || n.Text == "" {
			t.Fatalf("stall notice = %+v, want a warn-level explanation", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("silent running turn never produced a stall notice")
	}
	select {
	case <-notices:
		t.Fatal("stall notice repeated without any progress")
	case <-time.After(300 * time.Millisecond):
	}

	c.sink.Emit(event.Event{Kind: event.Text, Text: "still working"})
	select {
	case <-notices:
	case <-time.After(5 * time.Second):
		t.Fatal("renewed silence after progress did not warn again")
	}
	c.Cancel()
}

func TestIdleControllerNeverWarnsAboutStalls(t *testing.T) {
	notices := 0
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Code == event.NoticeCodeTurnStalled {
			notices++
		}
	})})
	t.Cleanup(c.Close)
	c.liveness.reset(time.Now().Add(-time.Hour))
	c.warnIfTurnStalled(time.Now())
	if notices != 0 {
		t.Fatalf("idle controller emitted %d stall notices", notices)
	}
}
