package agent

import (
	"sync"
	"testing"

	"reasonix/internal/provider"
)

// TestSessionConcurrentAddAndRead models the real hazard: the run loop appends
// messages while a frontend (serve /history, autosave) reads the log from another
// goroutine. Snapshot must copy under the lock; before it, an append racing the
// copy could tear the slice header and crash.
func TestSessionConcurrentAddAndRead(t *testing.T) {
	// Every Snapshot copies the whole log, so the work is readers*reads*appends.
	// -race instruments each of those accesses and the package shares one
	// 10-minute timeout; overlap is what detects the race, volume only buys
	// wall-clock.
	const (
		appends = 1000
		readers = 8
		reads   = 500
	)

	s := NewSession("sys")

	var wg sync.WaitGroup
	// One writer mimicking the turn goroutine.
	wg.Go(func() {
		for range appends {
			s.Add(provider.Message{Role: provider.RoleUser, Content: "msg"})
		}
	})
	// Many readers mimicking frontends polling history.
	for range readers {
		wg.Go(func() {
			for range reads {
				snap := s.Snapshot()
				for _, m := range snap { // iterate the copy: must never tear
					_ = m.Content
				}
				_ = s.HasContent()
			}
		})
	}
	wg.Wait()

	if got := len(s.Snapshot()); got != appends+1 { // appends + the system prompt
		t.Fatalf("final message count = %d, want %d", got, appends+1)
	}
}
