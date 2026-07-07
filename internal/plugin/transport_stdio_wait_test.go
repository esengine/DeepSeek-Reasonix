package plugin

import (
	"testing"
	"time"
)

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
