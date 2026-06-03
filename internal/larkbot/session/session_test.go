package session

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/event"
)

func TestNewRouterDefaults(t *testing.T) {
	r := NewRouter(Options{})
	if r.opts.SessionTTL != 1*time.Hour {
		t.Errorf("default TTL = %v, want 1h", r.opts.SessionTTL)
	}
	if r.Count() != 0 {
		t.Errorf("new router should have 0 sessions")
	}
}

func TestRouterCount(t *testing.T) {
	r := NewRouter(Options{})

	r.mu.Lock()
	r.sessions["a"] = &sessionHandle{chatID: "a", chatType: "p2p", lastUsed: time.Now()}
	r.sessions["b"] = &sessionHandle{chatID: "b", chatType: "group", lastUsed: time.Now()}
	r.mu.Unlock()

	if r.Count() != 2 {
		t.Errorf("count = %d, want 2", r.Count())
	}
}

func TestRouterGet(t *testing.T) {
	r := NewRouter(Options{})

	ctrl, sink := r.Get("nonexistent")
	if ctrl != nil || sink != nil {
		t.Error("Get on nonexistent should return nil")
	}
}

func TestRouterSweepExpired(t *testing.T) {
	r := NewRouter(Options{SessionTTL: 10 * time.Minute})

	r.mu.Lock()
	r.sessions["fresh"] = &sessionHandle{
		chatID:   "fresh",
		chatType: "p2p",
		lastUsed: time.Now(),
	}
	r.sessions["stale"] = &sessionHandle{
		chatID:   "stale",
		chatType: "group",
		lastUsed: time.Now().Add(-1 * time.Hour),
	}
	r.mu.Unlock()

	r.SweepExpired(context.Background())

	if r.Count() != 1 {
		t.Errorf("after sweep: %d sessions, want 1 (fresh only)", r.Count())
	}
	_, sink := r.Get("stale")
	if sink != nil {
		t.Error("stale session should return nil")
	}
}

func TestRouterEviction(t *testing.T) {
	r := NewRouter(Options{MaxSessions: 2})

	r.mu.Lock()
	r.sessions["oldest"] = &sessionHandle{
		chatID:   "oldest",
		chatType: "p2p",
		lastUsed: time.Now().Add(-2 * time.Hour),
	}
	r.sessions["middle"] = &sessionHandle{
		chatID:   "middle",
		chatType: "group",
		lastUsed: time.Now().Add(-1 * time.Hour),
	}
	r.mu.Unlock()

	r.mu.Lock()
	r.evictIfNeededLocked()
	r.mu.Unlock()

	if r.Count() != 1 {
		t.Errorf("after eviction: %d sessions, want 1", r.Count())
	}
}

func TestRouterNoEvictionUnderLimit(t *testing.T) {
	r := NewRouter(Options{MaxSessions: 5})

	r.mu.Lock()
	r.sessions["a"] = &sessionHandle{chatID: "a", chatType: "p2p", lastUsed: time.Now()}
	r.sessions["b"] = &sessionHandle{chatID: "b", chatType: "group", lastUsed: time.Now()}
	r.mu.Unlock()

	r.mu.Lock()
	r.evictIfNeededLocked()
	r.mu.Unlock()

	if r.Count() != 2 {
		t.Error("no eviction should happen when under limit")
	}
}

func TestRouterPermissionResolution(t *testing.T) {
	r := NewRouter(Options{
		GroupPermission: PermissionReadOnly,
		DMPermission:    PermissionInteractive,
	})

	if got := r.resolvePermission("group"); got != PermissionReadOnly {
		t.Errorf("group = %q, want %q", got, PermissionReadOnly)
	}
	if got := r.resolvePermission("p2p"); got != PermissionInteractive {
		t.Errorf("p2p = %q, want %q", got, PermissionInteractive)
	}
	if got := r.resolvePermission("unknown"); got != PermissionInteractive {
		t.Errorf("unknown = %q, want %q (fallback to DM)", got, PermissionInteractive)
	}
}

func TestSinkAdapterEmitDrain(t *testing.T) {
	sink := &SinkAdapter{}
	sink.Emit(event.Event{Kind: event.TurnStarted})
	sink.Emit(event.Event{Kind: event.Text, Text: "hello"})

	events := sink.Drain()
	if len(events) != 2 {
		t.Fatalf("drain: %d events, want 2", len(events))
	}
	if events[0].Kind != event.TurnStarted {
		t.Error("first event should be TurnStarted")
	}

	events = sink.Drain()
	if len(events) != 0 {
		t.Errorf("second drain: %d events, want 0", len(events))
	}
}

func TestSinkAdapterWaitForEventPreloaded(t *testing.T) {
	sink := &SinkAdapter{}
	sink.Emit(event.Event{Kind: event.Text, Text: "pre"})

	events, err := sink.WaitForEvent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}
