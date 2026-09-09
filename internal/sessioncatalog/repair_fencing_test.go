package sessioncatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func seedRepairFenceRow(t *testing.T, catalog *Catalog, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte("{\"role\":\"user\",\"content\":\"old\"}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	contentFingerprint := sessionContentFingerprint(path)
	metaFingerprint := fileFingerprint(agent.BranchMetaPath(path))
	record := SessionRecord{
		Path: path, Directory: filepath.Dir(path), Scope: "global", TurnsState: TurnsUnknown, Health: HealthOK,
		ContentFingerprint: contentFingerprint, MetaFingerprint: metaFingerprint,
	}
	if _, err := catalog.upsertSessionsWithNotification(context.Background(), []SessionRecord{record}, nil, "seed", false, upsertDirectoryProjection); err != nil {
		t.Fatal(err)
	}
}

func TestRepairBatchRejectsStaleClaimAfterSourceAdvance(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	catalog, err := Open(ctx, Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	path := filepath.Join(t.TempDir(), "stale-source.jsonl")
	saveLineageSession(t, path, "old", "answer")
	seedRepairFenceRow(t, catalog, path)
	items, err := catalog.claimDueRepairs(ctx, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("claimDueRepairs items=%d err=%v", len(items), err)
	}
	result := agent.SessionListingRepairResult{
		Status: agent.SessionListingRepairApplied, Preview: "old", Turns: 1,
		ContentFingerprint: sessionContentFingerprint(path), MetaFingerprint: fileFingerprint(agent.BranchMetaPath(path)),
	}
	beforeRevision := catalog.revision.Load()
	foreground, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	foreground.Add(provider.Message{Role: provider.RoleUser, Content: "new generation is longer"})
	if err := foreground.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	if err := catalog.applyRepairBatch(ctx, []repairOutcome{{item: items[0], result: result}}, map[string]DirectoryTarget{}); err != nil {
		t.Fatal(err)
	}
	var state string
	var turnsState TurnsState
	var preview, sourceFingerprint string
	if err := catalog.db.QueryRowContext(ctx, `SELECT repair_state,turns_state,preview,repair_source_fingerprint
		FROM catalog_sessions WHERE path_key=?`, catalog.pathKey(path)).Scan(&state, &turnsState, &preview, &sourceFingerprint); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || turnsState != TurnsUnknown || preview == "old" {
		t.Fatalf("stale result published: state=%s turns=%s preview=%q", state, turnsState, preview)
	}
	wantSource := sessionContentFingerprint(path) + "\x00" + fileFingerprint(agent.BranchMetaPath(path))
	if sourceFingerprint != wantSource {
		t.Fatalf("pending source = %q, want %q", sourceFingerprint, wantSource)
	}
	if catalog.revision.Load() != beforeRevision+1 {
		t.Fatalf("source reset revision = %d, want %d", catalog.revision.Load(), beforeRevision+1)
	}
}

func TestRepairBatchRejectsExpiredClaimOwner(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	dbPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	openCatalog := func() *Catalog {
		catalog, err := Open(ctx, Options{Path: dbPath, DisableRepair: true, Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = catalog.Close(context.Background()) })
		return catalog
	}
	first := openCatalog()
	path := filepath.Join(t.TempDir(), "expired-owner.jsonl")
	seedRepairFenceRow(t, first, path)
	oldItems, err := first.claimDueRepairs(ctx, 1)
	if err != nil || len(oldItems) != 1 {
		t.Fatalf("old claim items=%d err=%v", len(oldItems), err)
	}
	now = now.Add(30 * time.Second)
	second := openCatalog()
	newItems, err := second.claimDueRepairs(ctx, 1)
	if err != nil || len(newItems) != 1 {
		t.Fatalf("new claim items=%d err=%v", len(newItems), err)
	}
	fingerprint := sessionContentFingerprint(path)
	oldResult := agent.SessionListingRepairResult{
		Status: agent.SessionListingRepairApplied, Preview: "old owner", Turns: 1,
		ContentFingerprint: fingerprint, MetaFingerprint: fileFingerprint(agent.BranchMetaPath(path)),
	}
	beforeRevision := first.revision.Load()
	if err := first.applyRepairBatch(ctx, []repairOutcome{{item: oldItems[0], result: oldResult}}, map[string]DirectoryTarget{}); err != nil {
		t.Fatal(err)
	}
	var state, preview string
	var attempts int
	var retryAt int64
	if err := first.db.QueryRowContext(ctx, `SELECT repair_state,repair_attempts,repair_retry_at,preview
		FROM catalog_sessions WHERE path_key=?`, first.pathKey(path)).Scan(&state, &attempts, &retryAt, &preview); err != nil {
		t.Fatal(err)
	}
	if state != "active" || attempts != newItems[0].attempts || retryAt != newItems[0].retryAt || preview == "old owner" {
		t.Fatalf("old owner overwrote new claim: state=%s attempts=%d retry=%d preview=%q", state, attempts, retryAt, preview)
	}
	if first.revision.Load() != beforeRevision {
		t.Fatal("all-stale repair batch published an empty revision")
	}
	newResult := oldResult
	newResult.Preview = "new owner"
	if err := second.applyRepairBatch(ctx, []repairOutcome{{item: newItems[0], result: newResult}}, map[string]DirectoryTarget{}); err != nil {
		t.Fatal(err)
	}
	var turnsState TurnsState
	if err := second.db.QueryRowContext(ctx, `SELECT repair_state,turns_state,preview FROM catalog_sessions WHERE path_key=?`,
		second.pathKey(path)).Scan(&state, &turnsState, &preview); err != nil {
		t.Fatal(err)
	}
	if state != "complete" || turnsState != TurnsValid || preview != "new owner" {
		t.Fatalf("new owner did not complete: state=%s turns=%s preview=%q", state, turnsState, preview)
	}
}
