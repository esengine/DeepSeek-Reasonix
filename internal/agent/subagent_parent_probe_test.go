package agent

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSubagentStoreCleanupStaleRunningParentProbe(t *testing.T) {
	live, dead := true, false
	tests := []struct {
		name           string
		probeResult    *bool
		wantProbeCalls int
		wantCleaned    int
		wantStatus     SubagentStatus
	}{
		{name: "live parent", probeResult: &live, wantProbeCalls: 1, wantStatus: SubagentRunning},
		{name: "dead parent", probeResult: &dead, wantProbeCalls: 4, wantCleaned: 1, wantStatus: SubagentInterrupted},
		{name: "nil probe", wantCleaned: 1, wantStatus: SubagentInterrupted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessionDir := t.TempDir()
			store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
			spec := testSubagentSpec(t, "review")
			spec.ParentSession = "probe-parent"
			run, err := store.PrepareFresh(spec)
			if err != nil {
				t.Fatalf("PrepareFresh: %v", err)
			}
			if err := store.MarkRunning(run); err != nil {
				t.Fatalf("MarkRunning: %v", err)
			}
			ref := run.Ref
			run.Release()

			probeCalls := 0
			if tt.probeResult != nil {
				store.WithParentSessionProbe(func(path string) bool {
					probeCalls++
					wantPath := filepath.Join(sessionDir, spec.ParentSession+".jsonl")
					if filepath.Clean(path) != filepath.Clean(wantPath) {
						t.Fatalf("probe path = %q, want %q", path, wantPath)
					}
					return *tt.probeResult
				})
			}
			cleaned, err := store.CleanupStaleRunning()
			if err != nil {
				t.Fatalf("CleanupStaleRunning: %v", err)
			}
			if cleaned != tt.wantCleaned {
				t.Fatalf("cleaned = %d, want %d", cleaned, tt.wantCleaned)
			}
			if probeCalls != tt.wantProbeCalls {
				t.Fatalf("probe calls = %d, want %d", probeCalls, tt.wantProbeCalls)
			}
			meta, err := store.LoadMeta(ref)
			if err != nil {
				t.Fatalf("LoadMeta: %v", err)
			}
			if meta.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", meta.Status, tt.wantStatus)
			}
		})
	}
}

// TestIssue8372CleanupYieldsWhenParentBecomesLiveAfterProbe closes the
// check/use window reproduced during P0: a desktop tab can publish the parent
// session after cleanup's optimistic probe but before cleanup acquires its
// lease. The post-acquire probe must preserve the live subagent and release the
// lease for startup.
func TestIssue8372CleanupYieldsWhenParentBecomesLiveAfterProbe(t *testing.T) {
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	spec := testSubagentSpec(t, "issue-8372")
	spec.ParentSession = "parent"
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	if err := store.MarkRunning(run); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	ref := run.Ref
	run.Release()

	probeRead := make(chan struct{})
	returnCachedProbe := make(chan struct{})
	parentLive := false
	probeCalls := 0

	store.WithParentSessionProbe(func(path string) bool {
		wantPath := filepath.Join(sessionDir, spec.ParentSession+".jsonl")
		if filepath.Clean(path) != filepath.Clean(wantPath) {
			t.Errorf("probe path = %q, want %q", path, wantPath)
		}
		probeCalls++
		if probeCalls == 1 {
			observed := parentLive
			close(probeRead)
			<-returnCachedProbe
			return observed
		}
		return parentLive
	})
	store.cleanupBeforeReread = func(parentSession, gotRef string) {
		t.Errorf("cleanup reached metadata reread for live parent %q/%q", parentSession, gotRef)
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

	<-probeRead
	// This models a new desktop tab publishing the parent session after the
	// cleanup probe read false but before cleanup acquires the session lease.
	parentLive = true
	close(returnCachedProbe)
	result := <-cleanupDone

	if result.err != nil {
		t.Fatalf("CleanupStaleRunning: %v", result.err)
	}
	if result.cleaned != 0 {
		t.Fatalf("cleaned = %d, want 0 after parent became live", result.cleaned)
	}
	if probeCalls != 2 {
		t.Fatalf("probe calls = %d, want pre-lease and post-acquire checks", probeCalls)
	}

	parentPath := filepath.Join(sessionDir, spec.ParentSession+".jsonl")
	startupLease, startupErr := TryAcquireSessionLease(parentPath)
	if startupErr != nil {
		t.Fatalf("startup lease after cleanup yielded: %v", startupErr)
	}
	startupLease.Release()
	meta, err := store.LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.Status != SubagentRunning {
		t.Fatalf("status = %q, want running after parent became live", meta.Status)
	}
}

func TestCleanupStaleRunningRechecksLiveParentBeforeMetadataRewrite(t *testing.T) {
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	spec := testSubagentSpec(t, "review")
	spec.ParentSession = "parent"

	refs := make([]string, 0, 2)
	for range 2 {
		run, err := store.PrepareFresh(spec)
		if err != nil {
			t.Fatalf("PrepareFresh: %v", err)
		}
		if err := store.MarkRunning(run); err != nil {
			t.Fatalf("MarkRunning: %v", err)
		}
		refs = append(refs, run.Ref)
		run.Release()
	}

	parentLive := false
	probeCalls := 0
	store.WithParentSessionProbe(func(string) bool {
		probeCalls++
		return parentLive
	})
	store.cleanupBeforeReread = func(string, string) {
		parentLive = true
	}

	cleaned, err := store.CleanupStaleRunning()
	if err != nil {
		t.Fatalf("CleanupStaleRunning: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("cleaned = %d, want 0 when parent becomes live before metadata rewrite", cleaned)
	}
	if probeCalls != 3 {
		t.Fatalf("probe calls = %d, want pre-lease, post-acquire, and pre-write checks", probeCalls)
	}
	for _, ref := range refs {
		meta, err := store.LoadMeta(ref)
		if err != nil {
			t.Fatalf("LoadMeta(%s): %v", ref, err)
		}
		if meta.Status != SubagentRunning {
			t.Fatalf("status(%s) = %q, want running", ref, meta.Status)
		}
	}
}

func TestCleanupStaleRunningPublishesMaintenanceLease(t *testing.T) {
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	spec := testSubagentSpec(t, "maintenance")
	spec.ParentSession = "parent"
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	if err := store.MarkRunning(run); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	run.Release()

	cleanupHoldingLease := make(chan struct{})
	allowCleanup := make(chan struct{})
	store.cleanupBeforeReread = func(string, string) {
		close(cleanupHoldingLease)
		<-allowCleanup
	}
	cleanupDone := make(chan error, 1)
	go func() {
		_, err := store.CleanupStaleRunning()
		cleanupDone <- err
	}()
	<-cleanupHoldingLease

	parentPath := filepath.Join(sessionDir, spec.ParentSession+".jsonl")
	_, conflict := TryAcquireSessionLease(parentPath)
	if !errors.Is(conflict, ErrSessionLeaseHeld) {
		close(allowCleanup)
		<-cleanupDone
		t.Fatalf("startup conflict = %v, want ErrSessionLeaseHeld", conflict)
	}
	if !IsSessionMaintenanceLeaseConflict(conflict) {
		close(allowCleanup)
		<-cleanupDone
		t.Fatal("CleanupStaleRunning holder was not marked as maintenance")
	}

	close(allowCleanup)
	if err := <-cleanupDone; err != nil {
		t.Fatalf("CleanupStaleRunning: %v", err)
	}
}
