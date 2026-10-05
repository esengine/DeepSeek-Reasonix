package control

import (
	"context"
	"errors"
	"reasonix/internal/state/sessionstore"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
)

// TestCompactRefusedWhileRunning locks in the same guard Rewind/Branch have:
// the run loop is the only sanctioned writer of the live session during a
// turn, so a manual compact must be refused instead of rewriting the log
// underneath it.
func TestCompactRefusedWhileRunning(t *testing.T) {
	sess := sessionstore.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{
		Executor:   exec,
		SessionDir: testenv.TempDir(t),
		Label:      "test",
		Sink:       event.Discard,
	})

	c.mu.Lock()
	c.gate.running = true
	c.mu.Unlock()

	_, err := c.Compact(context.Background(), agent.CompactRequest{})
	if err == nil {
		t.Fatal("Compact while running should be refused")
	}
	if !strings.Contains(err.Error(), "cannot compact") {
		t.Fatalf("err = %v, want 'cannot compact' guard error", err)
	}
}

// TestRewindConcurrentWithHistoryReads exercises the conversation-rewind
// truncation against parallel History/CheckpointHasBoundary readers; before
// Rewind switched to Session.Snapshot/Replace the bare
// `s.Messages = s.Messages[:boundary]` write raced them (caught by -race).
func TestRewindConcurrentWithHistoryReads(t *testing.T) {
	c, ag, _ := runTwoTurns(t)

	c.checkpoints.mu.Lock()
	lastTurn := c.checkpoints.turn - 1
	c.checkpoints.mu.Unlock()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					_ = c.History()
					_ = c.CheckpointHasBoundary(lastTurn)
				}
			}
		})
	}

	err := c.Rewind(lastTurn, RewindConversation)
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatalf("Rewind: %v", err)
	}

	// The rewind truncated the log back to the last turn's boundary; History
	// must still serve a consistent snapshot afterwards.
	if got, want := ag.Session().Len(), 3; got != want { // sys + first prompt/answer
		t.Fatalf("messages after rewind = %d, want %d", got, want)
	}
}

// TestResumeRefusedWhileRunning is the guard the session switcher was missing.
// Every other session swap claims the rotation gate; Resume did not, so binding
// another transcript mid-turn left the run loop writing into it — which is how
// one conversation's output appeared in the one the user had switched to.
func TestResumeRefusedWhileRunning(t *testing.T) {
	live := sessionstore.NewSession("sys")
	live.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	exec := agent.New(nil, nil, live, agent.Options{}, event.Discard)
	c := New(Options{
		Executor:   exec,
		SessionDir: testenv.TempDir(t),
		Label:      "test",
		Sink:       event.Discard,
	})
	c.SetSessionPath("/tmp/a.jsonl")

	c.mu.Lock()
	c.gate.running = true
	c.mu.Unlock()

	other := sessionstore.NewSession("sys")
	if err := c.Resume(other, "/tmp/b.jsonl"); !errors.Is(err, errTurnRunningRotation) {
		t.Fatalf("Resume while running = %v, want errTurnRunningRotation", err)
	}
	// The refusal has to leave the live turn's binding untouched, not half-swap.
	if got := c.SessionPath(); got != "/tmp/a.jsonl" {
		t.Fatalf("SessionPath after refused resume = %q, want the running turn's own path", got)
	}

	c.mu.Lock()
	c.gate.running = false
	c.mu.Unlock()
	if err := c.Resume(other, "/tmp/b.jsonl"); err != nil {
		t.Fatalf("Resume once idle = %v, want nil", err)
	}
	if got := c.SessionPath(); got != "/tmp/b.jsonl" {
		t.Fatalf("SessionPath after resume = %q, want the resumed path", got)
	}
}
