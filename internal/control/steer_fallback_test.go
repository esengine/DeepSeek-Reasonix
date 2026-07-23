package control

import (
	"context"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func steerFallbackController(t *testing.T) (*Controller, *agent.Agent) {
	t.Helper()
	prov := &scriptedTurns{turns: [][]provider.Chunk{textTurn("ok")}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	return New(Options{Runner: ag, Executor: ag, Sink: event.Discard}), ag
}

func sessionHasUserText(ag *agent.Agent, text string) bool {
	for _, m := range ag.Session().Snapshot() {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, text) {
			return true
		}
	}
	return false
}

// TestSteerFallbackParksWhileRunning forces the turn-exit window: the
// controller still reports running (the previous body has not returned), but
// the agent's steer intake is already closed, so exec.Steer rejects the text.
// The fallback must park the steer as a turn and deliver it when the window
// closes — runGuarded's deliberately-silent running drop would lose the
// user's words.
func TestSteerFallbackParksWhileRunning(t *testing.T) {
	c, ag := steerFallbackController(t)

	block := make(chan struct{})
	started := make(chan struct{})
	c.runGuarded(func(context.Context) error {
		close(started)
		<-block
		return nil
	})
	<-started

	c.Steer("queued while exiting")

	c.mu.Lock()
	parked := len(c.parkedTurns)
	c.mu.Unlock()
	if parked != 1 {
		t.Fatalf("steer fallback should park while running, parked=%d", parked)
	}

	close(block)
	waitIdleAdmission(t, c)
	deadline := time.Now().Add(30 * time.Second)
	for !sessionHasUserText(ag, "queued while exiting") {
		if time.Now().After(deadline) {
			t.Fatalf("parked steer was never delivered as a turn")
		}
		time.Sleep(time.Millisecond)
		waitIdleAdmission(t, c)
	}
}

// TestSteerBetweenTurnsRunsNewTurn pins the idle fallback: with no turn
// running the executor rejects the steer, and the controller must submit it
// as a regular turn instead of returning silently (also covers the
// executor-rejection path a stale frontend running flag would hit).
func TestSteerBetweenTurnsRunsNewTurn(t *testing.T) {
	c, ag := steerFallbackController(t)

	c.Steer("late steer")

	deadline := time.Now().Add(30 * time.Second)
	for !sessionHasUserText(ag, "late steer") {
		if time.Now().After(deadline) {
			t.Fatalf("idle steer was never delivered as a turn")
		}
		time.Sleep(time.Millisecond)
		waitIdleAdmission(t, c)
	}
}
