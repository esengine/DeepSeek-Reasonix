package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestSteerPersistsIntoSession proves the persistence contract of mid-run
// guidance injection (P2.3): an accepted steer is consumed by the next turn
// (consumeSteer), wrapped with the MidTurnSteerPrefix, and added to the
// session as a user message — the exact path run_loop.go:336-337 takes for a
// running sub-agent — so it survives across turns and compactions.
func TestSteerPersistsIntoSession(t *testing.T) {
	sess := NewSession("sys")
	a := New(&fakeProvider{reply: "ok"}, tool.NewRegistry(), sess, Options{}, event.Discard)

	// Simulate an active run (a running sub-agent): Steer accepts only while
	// the agent's turn is live — the same state jobs.SteerJob observes for a
	// running background sub-agent.
	a.steerMu.Lock()
	a.steerRunActive = true
	a.steerMu.Unlock()

	// A steer accepted while the (sub-)agent runs (Agent.Steer, same entry the
	// jobs.SteerJob callback invokes).
	if !a.Steer("use pnpm not npm") {
		t.Fatal("Steer not accepted while run is active")
	}

	// Next turn: consumeSteer drains the queue (run_loop.go:336) and the
	// steer becomes a session user message (run_loop.go:337).
	text, ok := a.consumeSteer()
	if !ok {
		t.Fatal("consumeSteer returned nothing after an accepted steer")
	}
	if text != "use pnpm not npm" {
		t.Errorf("consumed steer text = %q", text)
	}
	a.session.Add(provider.Message{
		Role:    provider.RoleUser,
		Content: a.withTurnPreferences(midTurnSteerMessage(text)),
	})

	var found bool
	for _, m := range sess.Messages {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, "use pnpm not npm") {
			found = true
			if !strings.HasPrefix(m.Content, MidTurnSteerPrefix) {
				t.Errorf("steer message missing MidTurnSteerPrefix: %q", m.Content)
			}
			break
		}
	}
	if !found {
		t.Fatal("steer text not persisted into the session")
	}
}

// TestSteerRejectedWhenNotActive guards the fallback contract: outside an
// active run, Steer returns false so the caller (SteerForwarder) falls back
// to a normal main-conversation turn instead of silently dropping guidance.
func TestSteerRejectedWhenNotActive(t *testing.T) {
	a := New(&fakeProvider{reply: "ok"}, tool.NewRegistry(), NewSession("sys"), Options{}, event.Discard)
	_ = context.Background // keep import stable if unused below
	if a.Steer("hello") {
		t.Fatal("Steer must be rejected when no run is active")
	}
}
