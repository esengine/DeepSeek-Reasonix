package control

import (
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// newHistoryCacheTestController builds the smallest Controller that can run
// History() without dragging in the full boot stack. The provider / runner
// / sink are nil because History() short-circuits when executor is nil, and
// we only need the executor for the cache path. We mount an Agent on a
// fresh session so the underlying session is the real, mutex-guarded one.
func newHistoryCacheTestController(t *testing.T) *Controller {
	t.Helper()
	sess := agent.NewSession("")
	a := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	return &Controller{
		executor:         a,
		historyCacheTTL:  25 * time.Millisecond,
	}
}

// TestHistoryCacheHitsInBurst verifies that back-to-back History() calls in
// the same instant (the bursty accessor pattern — lastAssistantText in the
// hook Stop path, the TUI's replaySectionsFor on every render) share a
// single Snapshot copy.
func TestHistoryCacheHitsInBurst(t *testing.T) {
	c := newHistoryCacheTestController(t)
	sess := c.executor.Session()
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "hello"})

	first := c.History()
	second := c.History()
	third := c.History()

	// All three callers should see the same underlying slice — the cache
	// returns the same reference until the TTL elapses.
	if &first[0] != &second[0] || &second[0] != &third[0] {
		t.Fatalf("expected cached slice to be shared; got distinct backing arrays")
	}
	hits, misses := c.HistoryCacheStats()
	if hits < 2 {
		t.Fatalf("expected at least 2 cache hits, got hits=%d misses=%d", hits, misses)
	}
	if misses != 1 {
		t.Fatalf("expected exactly 1 cache miss (the first call), got %d", misses)
	}
}

// TestHistoryCacheTTLEvicts verifies that the cache re-snapshots after the
// TTL window, so a slow reader that comes back later sees the latest
// messages (in this test: a session that has grown since the cached
// snapshot).
func TestHistoryCacheTTLEvicts(t *testing.T) {
	c := newHistoryCacheTestController(t)
	// Push the TTL far out so we can deterministically advance time with
	// a sleep instead of mocking the clock — the test takes ~30ms
	// either way, well under any reasonable CI budget.
	c.historyCacheTTL = 10 * time.Millisecond

	sess := c.executor.Session()
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	first := c.History()
	if len(first) != 1 {
		t.Fatalf("first snapshot len = %d, want 1", len(first))
	}

	// Append after the first read; the cache still holds the 1-message
	// snapshot for the TTL window.
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "second"})
	stale := c.History()
	if len(stale) != 1 {
		t.Fatalf("stale cache len = %d, want 1 (cached snapshot)", len(stale))
	}

	// Sleep past the TTL and re-read; the cache should re-snapshot and
	// see both messages.
	time.Sleep(15 * time.Millisecond)
	fresh := c.History()
	if len(fresh) != 2 {
		t.Fatalf("post-TTL snapshot len = %d, want 2", len(fresh))
	}
}

// TestHistoryCacheInvalidatedByResume verifies that Resume (which rebinds the
// underlying session) invalidates the cache, so the very next History()
// call sees the freshly-loaded transcript rather than a stale copy of the
// previous session.
func TestHistoryCacheInvalidatedByResume(t *testing.T) {
	c := newHistoryCacheTestController(t)
	oldSess := c.executor.Session()
	oldSess.Add(provider.Message{Role: provider.RoleUser, Content: "stale"})

	// Prime the cache with the old session.
	c.History()

	// Build a fresh session and Resume it. Without the explicit
	// invalidate-on-Resume, the cache would still hold the old slice
	// until the TTL elapsed.
	newSess := agent.NewSession("")
	newSess.Add(provider.Message{Role: provider.RoleUser, Content: "fresh turn 1"})
	newSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "fresh turn 2"})
	c.Resume(newSess, "")

	got := c.History()
	if len(got) != 2 || got[0].Content != "fresh turn 1" {
		t.Fatalf("post-Resume History = %+v, want the 2-message fresh session", got)
	}
}
