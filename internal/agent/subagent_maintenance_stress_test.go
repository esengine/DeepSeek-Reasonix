package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestIssue8372MaintenanceHandoffStress covers 0, 1, and multiple persisted
// running subagents across 100 cleanup/startup handoffs. A cleanup lease must
// be identified as maintenance, release its exact generation signal, and leave
// the parent session immediately reusable after both close and restart.
func TestIssue8372MaintenanceHandoffStress(t *testing.T) {
	const rounds = 100
	root := t.TempDir()
	counts := []int{0, 1, 6}

	for round := range rounds {
		count := counts[round%len(counts)]
		sessionDir := filepath.Join(root, fmt.Sprintf("round-%03d", round))
		store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
		spec := testSubagentSpec(t, "stress")
		spec.ParentSession = "parent"
		refs := make([]string, 0, count)
		for range count {
			run, err := store.PrepareFresh(spec)
			if err != nil {
				t.Fatalf("round %d PrepareFresh: %v", round, err)
			}
			if err := store.MarkRunning(run); err != nil {
				run.Release()
				t.Fatalf("round %d MarkRunning: %v", round, err)
			}
			refs = append(refs, run.Ref)
			run.Release()
		}

		parentPath := filepath.Join(sessionDir, spec.ParentSession+".jsonl")
		if count == 0 {
			cleaned, err := store.CleanupStaleRunning()
			if err != nil || cleaned != 0 {
				t.Fatalf("round %d empty cleanup = %d, %v; want 0, nil", round, cleaned, err)
			}
			assertSessionLeaseReusableAfterRestart(t, round, parentPath)
			continue
		}

		cleanupHoldingLease := make(chan struct{})
		allowCleanup := make(chan struct{})
		var gate sync.Once
		store.cleanupBeforeReread = func(string, string) {
			gate.Do(func() {
				close(cleanupHoldingLease)
				<-allowCleanup
			})
		}
		type cleanupResult struct {
			cleaned int
			err     error
		}
		cleanupDone := make(chan cleanupResult, 1)
		go func() {
			cleaned, err := store.CleanupStaleRunning()
			cleanupDone <- cleanupResult{cleaned: cleaned, err: err}
		}()
		<-cleanupHoldingLease

		_, conflict := TryAcquireSessionLease(parentPath)
		if !IsSessionMaintenanceLeaseConflict(conflict) {
			close(allowCleanup)
			<-cleanupDone
			t.Fatalf("round %d conflict = %v, want maintenance lease", round, conflict)
		}
		waitDone := make(chan bool, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			waitDone <- WaitForSessionMaintenanceLeaseRelease(ctx, conflict)
		}()
		close(allowCleanup)
		if !<-waitDone {
			result := <-cleanupDone
			t.Fatalf("round %d maintenance release timed out; cleanup = %+v", round, result)
		}
		result := <-cleanupDone
		if result.err != nil || result.cleaned != count {
			t.Fatalf("round %d cleanup = %d, %v; want %d, nil", round, result.cleaned, result.err, count)
		}
		for _, ref := range refs {
			meta, err := store.LoadMeta(ref)
			if err != nil {
				t.Fatalf("round %d LoadMeta(%s): %v", round, ref, err)
			}
			if meta.Status != SubagentInterrupted {
				t.Fatalf("round %d status(%s) = %q, want interrupted", round, ref, meta.Status)
			}
		}
		assertSessionLeaseReusableAfterRestart(t, round, parentPath)
	}
}

func assertSessionLeaseReusableAfterRestart(t *testing.T, round int, path string) {
	t.Helper()
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("round %d lease after cleanup: %v", round, err)
	}
	lease.Release()
	if SessionLeaseHeldByCurrentRuntime(path) {
		t.Fatalf("round %d session stayed busy after close", round)
	}
	restarted, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("round %d lease after restart: %v", round, err)
	}
	restarted.Release()
}
