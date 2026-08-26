package sessioncatalog

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/agent"
)

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

	// Hold the directory lock so the queued reconcile cannot hide a direct
	// revision or projection mutation from this assertion.
	lock := catalog.directoryLock(target.Path)
	lock.Lock()
	revision := catalog.Status().Revision
	if err := catalog.applyRepairResult(ctx, leaf, "repaired preview", 2, true); err != nil {
		lock.Unlock()
		t.Fatal(err)
	}
	mid, ok, err := catalog.GetSession(ctx, leaf)
	if err != nil || !ok {
		lock.Unlock()
		t.Fatalf("GetSession during repair: ok=%v err=%v", ok, err)
	}
	midPage, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50})
	if err != nil {
		lock.Unlock()
		t.Fatal(err)
	}
	if catalog.Status().Revision != revision {
		lock.Unlock()
		t.Fatalf("repair published revision %d, want unchanged %d", catalog.Status().Revision, revision)
	}
	assertDirectoryProjectionEqual(t, mid, before)
	if len(midPage.Items) != 1 || midPage.Items[0].TopicID != beforePage.Items[0].TopicID ||
		midPage.Items[0].RepresentativePath != beforePage.Items[0].RepresentativePath {
		lock.Unlock()
		t.Fatalf("repair changed topic projection: before=%+v during=%+v", beforePage.Items, midPage.Items)
	}
	lock.Unlock()

	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	after, ok, err := catalog.GetSession(ctx, leaf)
	if err != nil || !ok {
		t.Fatalf("GetSession after reconcile: ok=%v err=%v", ok, err)
	}
	assertDirectoryProjectionEqual(t, after, before)
	if after.TurnsState != TurnsValid || after.Turns != 2 || after.Preview != "repaired preview" {
		t.Fatalf("repaired source state was not retained: %+v", after)
	}
	signature, err := directorySignature(target.Path)
	if err != nil {
		t.Fatal(err)
	}
	if skip, err := catalog.directoryScanCanSkip(ctx, target, signature); err != nil || !skip {
		t.Fatalf("stable repaired projection cannot skip: skip=%v err=%v", skip, err)
	}
}

func TestRepairWaitsForDirectoryProjectionCommit(t *testing.T) {
	ctx := context.Background()
	catalog, target, root, leaf := openLegacyRecoveryCatalog(t, ctx)
	if err := agent.UpdateBranchMeta(root, false, func(meta *agent.BranchMeta) error {
		meta.Preview = "force reconcile"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	reconcilePaused := make(chan struct{})
	releaseReconcile := make(chan struct{})
	var pauseOnce sync.Once
	catalog.testReconcileBatchHook = func(int) {
		pauseOnce.Do(func() {
			close(reconcilePaused)
			<-releaseReconcile
		})
	}
	reconcileDone := make(chan error, 1)
	go func() { reconcileDone <- catalog.ReconcileDirectory(ctx, target) }()
	<-reconcilePaused

	repairAttempted := make(chan struct{})
	repairAcquired := make(chan struct{})
	catalog.testRepairLockHook = func(acquired bool) {
		if acquired {
			close(repairAcquired)
			return
		}
		close(repairAttempted)
	}
	repairDone := make(chan struct{})
	go func() {
		catalog.repairSession(ctx, leaf)
		close(repairDone)
	}()
	<-repairAttempted
	select {
	case <-repairAcquired:
		t.Fatal("repair acquired the directory lock before reconcile committed")
	default:
	}

	close(releaseReconcile)
	if err := <-reconcileDone; err != nil {
		t.Fatal(err)
	}
	<-repairAcquired
	<-repairDone
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	after, ok, err := catalog.GetSession(ctx, leaf)
	if err != nil || !ok || after.TurnsState != TurnsValid {
		t.Fatalf("repair/reconcile did not converge: record=%+v ok=%v err=%v", after, ok, err)
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
