package control

import (
	"context"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TestCompactRefusedWhileRunning locks in the same guard Rewind/Branch have:
// the run loop is the only sanctioned writer of the live session during a
// turn, so a manual compact must be refused instead of rewriting the log
// underneath it.
func TestCompactRefusedWhileRunning(t *testing.T) {
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{
		Executor:   exec,
		SessionDir: t.TempDir(),
		Label:      "test",
		Sink:       event.Discard,
	})

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	err := c.Compact(context.Background(), "")
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
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = c.History()
					_ = c.CheckpointHasBoundary(lastTurn)
				}
			}
		}()
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
