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
	s := NewSession("sys")

	var wg sync.WaitGroup
	// One writer mimicking the turn goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5000; i++ {
			s.Add(provider.Message{Role: provider.RoleUser, Content: "msg"})
		}
	}()
	// Many readers mimicking frontends polling history.
	for r := 0; r < 16; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5000; i++ {
				snap := s.Snapshot()
				for _, m := range snap { // iterate the copy: must never tear
					_ = m.Content
				}
				_ = s.HasContent()
			}
		}()
	}
	wg.Wait()

	if got := len(s.Snapshot()); got != 5001 { // 5000 + the system prompt
		t.Fatalf("final message count = %d, want 5001", got)
	}
}
