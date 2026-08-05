package agent

import (
	"path/filepath"
	"testing"
)

func TestSubagentStoreCleanupStaleRunningParentProbe(t *testing.T) {
	live, dead := true, false
	tests := []struct {
		name        string
		probeResult *bool
		wantCleaned int
		wantStatus  SubagentStatus
	}{
		{name: "live parent", probeResult: &live, wantStatus: SubagentRunning},
		{name: "dead parent", probeResult: &dead, wantCleaned: 1, wantStatus: SubagentInterrupted},
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
			if tt.probeResult != nil && probeCalls != 1 {
				t.Fatalf("probe calls = %d, want 1", probeCalls)
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
