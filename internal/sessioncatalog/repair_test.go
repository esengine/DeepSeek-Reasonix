package sessioncatalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
)

func TestRepairBackoffPersistsAndSourceChangesResetIt(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.jsonl")
	saveLineageSession(t, path, "question", "answer")
	if err := agent.UpdateBranchMeta(path, false, func(meta *agent.BranchMeta) error {
		meta.Revision++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	target := DirectoryTarget{Path: dir, Scope: "global"}
	catalog, err := Open(ctx, Options{Path: dbPath, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	retryAt := time.Now().Add(17 * time.Minute).UnixMilli()
	if _, err := catalog.db.ExecContext(ctx, `UPDATE catalog_sessions SET repair_state='deferred',repair_attempts=4,
		repair_retry_at=?,repair_error_kind='busy',repair_engine_version=? WHERE path_key=?`, retryAt, repairEngineVersion, catalog.pathKey(path)); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Close(ctx); err != nil {
		t.Fatal(err)
	}

	catalog, err = Open(ctx, Options{Path: dbPath, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	assertRepairSchedule := func(wantState string, wantAttempts int, wantRetry int64) {
		t.Helper()
		var state string
		var attempts int
		var retry int64
		if err := catalog.db.QueryRowContext(ctx, `SELECT repair_state,repair_attempts,repair_retry_at
			FROM catalog_sessions WHERE path_key=?`, catalog.pathKey(path)).Scan(&state, &attempts, &retry); err != nil {
			t.Fatal(err)
		}
		if state != wantState || attempts != wantAttempts || retry != wantRetry {
			t.Fatalf("repair schedule = %s/%d/%d, want %s/%d/%d", state, attempts, retry, wantState, wantAttempts, wantRetry)
		}
	}
	assertRepairSchedule("deferred", 4, retryAt)

	if err := os.WriteFile(agent.SessionEventLogPath(path), []byte("foreign event artifact\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := catalog.IndexSessionPath(ctx, target, path); err != nil {
		t.Fatal(err)
	}
	assertRepairSchedule("pending", 0, 0)

	if _, err := catalog.db.ExecContext(ctx, `UPDATE catalog_sessions SET repair_state='deferred',repair_attempts=2,
		repair_retry_at=?,repair_error_kind='io',repair_source_fingerprint=content_fingerprint||char(0)||meta_fingerprint
		WHERE path_key=?`, retryAt, catalog.pathKey(path)); err != nil {
		t.Fatal(err)
	}
	if err := agent.UpdateBranchMeta(path, false, func(meta *agent.BranchMeta) error {
		meta.CustomTitle = "changed meta generation"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.IndexSessionPath(ctx, target, path); err != nil {
		t.Fatal(err)
	}
	assertRepairSchedule("pending", 0, 0)
}

func TestDeferredRepairIsNotReopenedBeforeRetryAt(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	catalog, err := Open(ctx, Options{
		Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	record := SessionRecord{
		Path: filepath.Join(dir, "large.jsonl"), Directory: dir, Scope: "global",
		TurnsState: TurnsUnknown, Health: HealthOK,
	}
	if _, err := catalog.upsertSessionsWithNotification(ctx, []SessionRecord{record}, nil, "seed", false, upsertDirectoryProjection); err != nil {
		t.Fatal(err)
	}
	var calls int
	catalog.testRepairSessionHook = func(context.Context, string) (agent.SessionListingRepairResult, error) {
		calls++
		return agent.SessionListingRepairResult{}, agent.ErrSessionListingRepairBusy
	}
	catalog.runRepairWave(ctx)
	catalog.runRepairWave(ctx)
	if calls != 1 {
		t.Fatalf("repair opened %d times before retry_at, want 1", calls)
	}
	now = now.Add(30 * time.Second)
	catalog.runRepairWave(ctx)
	if calls != 2 {
		t.Fatalf("repair calls after retry_at = %d, want 2", calls)
	}
}

func TestRepairWaveBatches1056RowsAndDoesNotStarveAfterBlockedRow(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	records := make([]SessionRecord, 1056)
	for i := range records {
		records[i] = SessionRecord{
			Path: filepath.Join(dir, fmt.Sprintf("%04d.jsonl", i)), Directory: dir, Scope: "global",
			TurnsState: TurnsUnknown, Health: HealthOK, LastActivityAt: int64(1056 - i),
		}
	}
	if _, err := catalog.upsertSessionsWithNotification(ctx, records, nil, "seed", false, upsertDirectoryProjection); err != nil {
		t.Fatal(err)
	}
	failingPath := records[0].Path
	catalog.testRepairSessionHook = func(_ context.Context, path string) (agent.SessionListingRepairResult, error) {
		if path == failingPath {
			return agent.SessionListingRepairResult{Status: agent.SessionListingRepairUnsupported}, nil
		}
		return agent.SessionListingRepairResult{Status: agent.SessionListingRepairApplied, Preview: "ok", Turns: 1}, nil
	}
	var reconciles atomic.Int64
	catalog.testReconcileStartHook = func(DirectoryTarget) { reconciles.Add(1) }
	lock := catalog.directoryLock(dir)
	lock.Lock()
	unlocked := false
	defer func() {
		if !unlocked {
			lock.Unlock()
		}
	}()

	catalog.runRepairWave(ctx)
	var valid, blocked int
	if err := catalog.db.QueryRowContext(ctx, `SELECT
		SUM(CASE WHEN turns_state='valid' THEN 1 ELSE 0 END),
		SUM(CASE WHEN turns_state='unknown' AND repair_state='blocked' THEN 1 ELSE 0 END)
		FROM catalog_sessions`).Scan(&valid, &blocked); err != nil {
		t.Fatal(err)
	}
	if valid != 1055 || blocked != 1 {
		t.Fatalf("repair wave valid/blocked = %d/%d, want 1055/1", valid, blocked)
	}
	deadline := time.Now().Add(2 * time.Second)
	for reconciles.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := reconciles.Load(); got != 1 {
		t.Fatalf("queued reconciles = %d, want exactly 1", got)
	}
	time.Sleep(300 * time.Millisecond)
	if got := reconciles.Load(); got != 1 {
		t.Fatalf("duplicate reconcile started during one wave: %d", got)
	}
	lock.Unlock()
	unlocked = true
}

func TestRepairResultPreservesDirectoryProjectionUntilReconcile(t *testing.T) {
	ctx := context.Background()
	catalog, target, _, leaf := openLegacyRecoveryCatalog(t, ctx)

	before, ok, err := catalog.GetSession(ctx, leaf)
	if err != nil || !ok {
		t.Fatalf("GetSession before repair: ok=%v err=%v", ok, err)
	}
	beforePage, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50})
	if err != nil || len(beforePage.Items) != 1 {
		t.Fatalf("ListTopics before repair: items=%+v err=%v", beforePage.Items, err)
	}

	catalog.repairSession(ctx, leaf)
	mid, ok, err := catalog.GetSession(ctx, leaf)
	if err != nil || !ok {
		t.Fatalf("GetSession during repair: ok=%v err=%v", ok, err)
	}
	midPage, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	assertDirectoryProjectionEqual(t, mid, before)
	if len(midPage.Items) != 1 || midPage.Items[0].TopicID != beforePage.Items[0].TopicID ||
		midPage.Items[0].RepresentativePath != beforePage.Items[0].RepresentativePath {
		t.Fatalf("repair changed topic projection: before=%+v during=%+v", beforePage.Items, midPage.Items)
	}

	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	after, ok, err := catalog.GetSession(ctx, leaf)
	if err != nil || !ok {
		t.Fatalf("GetSession after reconcile: ok=%v err=%v", ok, err)
	}
	if after.TurnsState != TurnsValid || after.Turns != 2 || after.Preview != "question" {
		t.Fatalf("repaired source state was not retained: %+v", after)
	}
	if after.RecoveryDigest == "" || after.RecoveryRole != RecoveryRoleAdopted || !after.RecoveryCanonical {
		t.Fatalf("reconcile did not publish repaired recovery lineage: %+v", after)
	}
	signature, err := directorySignature(target.Path)
	if err != nil {
		t.Fatal(err)
	}
	if skip, err := catalog.directoryScanCanSkip(ctx, target, signature); err != nil || !skip {
		t.Fatalf("stable repaired projection cannot skip: skip=%v err=%v", skip, err)
	}
}

func TestRepairDoesNotWaitForDirectoryProjectionLock(t *testing.T) {
	ctx := context.Background()
	catalog, target, _, leaf := openLegacyRecoveryCatalog(t, ctx)
	lock := catalog.directoryLock(target.Path)
	lock.Lock()
	defer lock.Unlock()
	repairDone := make(chan struct{})
	go func() {
		catalog.repairSession(ctx, leaf)
		close(repairDone)
	}()
	select {
	case <-repairDone:
	case <-time.After(2 * time.Second):
		t.Fatal("repair waited for the directory projection lock")
	}
}

func openLegacyRecoveryCatalog(t *testing.T, ctx context.Context) (*Catalog, DirectoryTarget, string, string) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "root.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	saveLineageSession(t, root, "question", "answer")
	saveLineageSession(t, leaf, "question", "answer", "follow up", "done")
	if err := agent.SaveBranchMetaPreserveUpdated(root, agent.BranchMeta{
		ID: "root", Scope: "global", TopicID: "root-topic", TopicTitle: "Root",
		SchemaVersion: agent.BranchMetaCountsVersion, Turns: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(leaf, agent.BranchMeta{
		ID: "leaf", Scope: "global", TopicID: "leaf-topic", TopicTitle: "Leaf",
		Recovered: true, ParentID: "root", RecoveryDepth: 1, SchemaVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	target := DirectoryTarget{Path: dir, Scope: "global"}
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	return catalog, target, root, leaf
}

func assertDirectoryProjectionEqual(t *testing.T, got, want SessionRecord) {
	t.Helper()
	if got.TopicID != want.TopicID || got.TopicTitle != want.TopicTitle ||
		got.RecoveryCopy != want.RecoveryCopy || got.RecoveryGroupID != want.RecoveryGroupID ||
		got.RecoveryRole != want.RecoveryRole || got.RecoveryCanonical != want.RecoveryCanonical ||
		got.LogicalTopicID != want.LogicalTopicID || got.OrdinaryVisible != want.OrdinaryVisible {
		t.Fatalf("directory projection changed: got=%+v want=%+v", got, want)
	}
}
