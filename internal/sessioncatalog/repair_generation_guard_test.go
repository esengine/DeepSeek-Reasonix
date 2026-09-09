package sessioncatalog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
)

func TestRepairBatchHoldsSourceGenerationThroughCommit(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	catalog, err := Open(ctx, Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	path := filepath.Join(t.TempDir(), "commit-fence.jsonl")
	seedRepairFenceRow(t, catalog, path)
	items, err := catalog.claimDueRepairs(ctx, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("claimDueRepairs items=%d err=%v", len(items), err)
	}
	result := agent.SessionListingRepairResult{
		Status: agent.SessionListingRepairApplied, Preview: "old", Turns: 1,
		ContentFingerprint: sessionContentFingerprint(path), MetaFingerprint: fileFingerprint(agent.BranchMetaPath(path)),
	}
	catalog.testRepairBatchError = func(stage string) error {
		if stage != "commit" {
			return nil
		}
		_, unlock, lockErr := agent.TryLockSessionListingGeneration(path)
		if unlock != nil {
			unlock()
		}
		if !errors.Is(lockErr, agent.ErrSessionListingRepairBusy) {
			return errors.New("source generation was not fenced through commit")
		}
		return nil
	}
	if err := catalog.applyRepairBatch(ctx, []repairOutcome{{item: items[0], result: result}}, map[string]DirectoryTarget{}); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := catalog.db.QueryRowContext(ctx, `SELECT repair_state FROM catalog_sessions WHERE path_key=?`,
		catalog.pathKey(path)).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "complete" {
		t.Fatalf("repair state = %s, want complete", state)
	}
}
