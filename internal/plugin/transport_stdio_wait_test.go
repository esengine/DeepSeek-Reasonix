package plugin

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type countedWriteCloser struct {
	closes atomic.Int32
}

func (*countedWriteCloser) Write(p []byte) (int, error) { return len(p), nil }

func (c *countedWriteCloser) Close() error {
	c.closes.Add(1)
	return nil
}

// TestWaitWithBudgetReturnsWithinBudget pins that the budgeted reap helper
// returns even when the underlying wait never completes. withStderr calls this
// while holding callMu, and a surviving grandchild can keep cmd.Wait blocked
// forever — without the budget every future call on the transport would wedge.
func TestWaitWithBudgetReturnsWithinBudget(t *testing.T) {
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) }) // let the abandoned goroutine exit

	done := make(chan struct{})
	go func() {
		waitWithBudget(func() { <-blocked }, 100*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waitWithBudget did not return within its budget — callMu would wedge")
	}
}

// TestWaitWithBudgetReturnsEarlyWhenWaitCompletes pins that a fast wait returns
// promptly rather than always paying the full budget.
func TestWaitWithBudgetReturnsEarlyWhenWaitCompletes(t *testing.T) {
	done := make(chan struct{})
	go func() {
		waitWithBudget(func() {}, 10*time.Second)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waitWithBudget blocked on a wait that already completed")
	}
}

func TestStdioTransportConcurrentCloseConsumesOwnedResourcesOnce(t *testing.T) {
	cleanupDir := filepath.Join(t.TempDir(), "private-state")
	if err := os.Mkdir(cleanupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stdin := &countedWriteCloser{}
	var releaseCalls atomic.Int32
	transport := &stdioTransport{
		stdin:       stdin,
		releaseSlot: func() { releaseCalls.Add(1) },
		cleanupDir:  cleanupDir,
	}

	var closes sync.WaitGroup
	for range 32 {
		closes.Add(1)
		go func() {
			defer closes.Done()
			transport.close()
		}()
	}
	closes.Wait()

	if got := stdin.closes.Load(); got != 1 {
		t.Fatalf("stdin close calls = %d, want 1", got)
	}
	if got := releaseCalls.Load(); got != 1 {
		t.Fatalf("slot release calls = %d, want 1", got)
	}
	if _, err := os.Stat(cleanupDir); !os.IsNotExist(err) {
		t.Fatalf("private cleanup dir still exists or stat failed unexpectedly: %v", err)
	}
	if transport.releaseSlot != nil || transport.cleanupDir != "" {
		t.Fatal("close retained consumed stdio transport resources")
	}
}
