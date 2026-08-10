package autoresearch

import (
	"fmt"
	"testing"
	"time"
)

// TestFindingsTailEquivalence: newest-N reads match a full scan followed by a
// reverse/limit without writing through a production API.
func TestFindingsTailEquivalence(t *testing.T) {
	root := t.TempDir()
	taskID := "findings-tail"
	taskRoot := writeArchiveFixture(t, root, taskID, "Findings tail equivalence", nil)
	for i := 1; i <= 40; i++ {
		appendFindingLine(t, taskRoot, Finding{
			ID:        fmt.Sprintf("f%d", i),
			Kind:      "manual",
			Summary:   fmt.Sprintf("finding %d", i),
			Source:    FindingSourceManual,
			Accepted:  true,
			CreatedAt: time.Date(2026, 6, 30, 10, 0, i, 0, time.UTC),
		})
	}
	store := NewStore(root)
	all, err := store.Findings(taskID, 0)
	if err != nil {
		t.Fatalf("Findings full: %v", err)
	}
	tail, err := store.Findings(taskID, 5)
	if err != nil {
		t.Fatalf("Findings tail: %v", err)
	}
	if len(tail) != 5 {
		t.Fatalf("tail len = %d", len(tail))
	}
	// Newest-first order: first of tail is the latest full entry.
	if tail[0].ID != all[0].ID || tail[4].ID != all[4].ID {
		t.Fatalf("tail=%+v all[:5]=%+v", tail, all[:5])
	}
}

func TestHeartbeatTailEquivalence(t *testing.T) {
	root := t.TempDir()
	taskID := "heartbeat-tail"
	taskRoot := writeArchiveFixture(t, root, taskID, "Tail read equivalence", nil)
	for i := 1; i <= 30; i++ {
		appendHeartbeatLine(t, taskRoot, Heartbeat{
			Status:    HeartbeatTurnDone,
			Iteration: i,
			CreatedAt: time.Date(2026, 6, 30, 10, 0, i, 0, time.UTC),
		})
	}
	store := NewStore(root)
	all, err := store.Heartbeats(taskID, 0)
	if err != nil {
		t.Fatalf("Heartbeats full: %v", err)
	}
	tail, err := store.Heartbeats(taskID, 3)
	if err != nil {
		t.Fatalf("Heartbeats tail: %v", err)
	}
	if len(tail) != 3 {
		t.Fatalf("tail len = %d", len(tail))
	}
	want := all[len(all)-3:]
	for i := range want {
		if tail[i].Iteration != want[i].Iteration {
			t.Fatalf("tail[%d]=%+v want %+v", i, tail[i], want[i])
		}
	}
}

func TestDirectionsFileIsReadableThroughSummary(t *testing.T) {
	root := t.TempDir()
	taskID := "directions-readable"
	taskRoot := writeArchiveFixture(t, root, taskID, "Legacy fingerprint migration", nil)
	summary := "Benchmark the desktop frontend markdown rendering pipeline for large tables"
	writeDirections(t, taskRoot, []DirectionTried{{
		Fingerprint:        "legacy-slug",
		Summary:            summary,
		FirstSeenIteration: 1,
		LastSeenIteration:  1,
		Count:              1,
	}})
	writeProgress(t, taskRoot, Progress{
		Status:           StatusRunning,
		Iteration:        1,
		CurrentDirection: summary,
		UpdatedAt:        time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
	})
	store := NewStore(root)
	got, err := store.Summary(taskID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if got.CurrentDirection != summary || got.Iteration != 1 {
		t.Fatalf("summary = %+v", got)
	}
}
