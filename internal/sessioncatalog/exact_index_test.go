package sessioncatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
)

func TestIndexSessionPathSkipsUnchangedProjection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		Scope: "global", TopicID: "topic", TopicTitle: "Chat",
		SchemaVersion: agent.BranchMetaCountsVersion, Turns: 1,
	}); err != nil {
		t.Fatal(err)
	}
	events := 0
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true, OnRevision: func(uint64, []string, string) { events++ }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(ctx) })
	target := DirectoryTarget{Path: dir, Scope: "global"}
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	revision := catalog.Status().Revision
	before, _, _ := catalog.GetSession(ctx, path)
	for range 5 {
		if err := catalog.IndexSessionPath(ctx, target, path); err != nil {
			t.Fatal(err)
		}
	}
	if got := catalog.Status().Revision; got != revision {
		after, _, _ := catalog.GetSession(ctx, path)
		t.Logf("before=%+v after=%+v", before, after)
		t.Fatalf("revision after unchanged exact indexes = %d, want %d", got, revision)
	}
	if events != 1 {
		t.Fatalf("revision events after unchanged exact indexes = %d, want 1", events)
	}
}

func TestIndexSessionPathDoesNotRequeueRepairAfterRepair(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{TopicID: "topic", SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{InMemory: true, QueueCapacity: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(ctx) })
	target := DirectoryTarget{Path: dir, Scope: "global"}
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if catalog.Status().RepairPending == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if catalog.Status().RepairPending != 0 {
		t.Fatal("repair did not complete")
	}
	for range 20 {
		if err := catalog.IndexSessionPath(ctx, target, path); err != nil {
			t.Fatal(err)
		}
	}
	if got := catalog.Status().RepairPending; got != 0 {
		t.Fatalf("repair pending after unchanged exact indexes = %d", got)
	}
}

func TestExactIndexDoesNotDowngradeKnownCounts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(ctx) })
	record := SessionRecord{Path: "/sessions/chat.jsonl", Directory: "/sessions", Scope: "global", TopicID: "topic", CreatedAt: 1, LastActivityAt: 2, Preview: "hi", Turns: 1, TurnsState: TurnsValid, ContentFingerprint: "10:1", MetaFingerprint: "20:1", Health: HealthOK}
	if err := catalog.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	record.Preview, record.Turns, record.TurnsState, record.MetaFingerprint = "", 0, TurnsUnknown, "20:2"
	record.Recovered, record.RecoveryReason, record.RecoveryDigest, record.ParentID = true, "recovery", "digest", "parent"
	if err := catalog.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	got, ok, err := catalog.GetSession(ctx, record.Path)
	if err != nil || !ok {
		t.Fatalf("GetSession: ok=%v err=%v", ok, err)
	}
	if got.TurnsState != TurnsValid || got.Turns != 1 || got.Preview != "hi" ||
		!got.Recovered || got.RecoveryReason != "recovery" || got.RecoveryDigest != "digest" || got.ParentID != "parent" {
		t.Fatalf("exact index lost known counts or recovery metadata: %+v", got)
	}
}

func TestRecordFromOrderRejectsPreviousGenerationListingProjection(t *testing.T) {
	record := recordFromOrder(DirectoryTarget{Path: "/sessions", Scope: "global"}, agent.SessionOrderInfo{
		Path:                 "/sessions/chat.jsonl",
		Scope:                "global",
		Preview:              "stale preview",
		Turns:                7,
		SchemaVersion:        agent.BranchMetaCountsVersion,
		Revision:             2,
		ContentDigest:        "new-digest",
		ListingRevision:      1,
		ListingContentDigest: "old-digest",
	})
	if record.TurnsState != TurnsUnknown || record.Turns != 0 || record.Preview != "" {
		t.Fatalf("stale listing projection remained visible: %+v", record)
	}
}

func TestExactIndexDoesNotRegressKnownActivity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	current := created.Add(15 * 24 * time.Hour)
	if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
		CreatedAt: created, UpdatedAt: current, Scope: "global", TopicID: "topic",
		TopicTitle: "Chat", SchemaVersion: agent.BranchMetaCountsVersion, Turns: 1,
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(ctx) })
	target := DirectoryTarget{Path: dir, Scope: "global"}
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	// A transient sidecar read can expose an older timestamp while the
	// authoritative transcript is unchanged. Exact indexing must not move the
	// conversation backwards in the sidebar.
	if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
		CreatedAt: created, UpdatedAt: created.Add(30 * time.Hour), Scope: "global", TopicID: "topic",
		TopicTitle: "Chat", SchemaVersion: agent.BranchMetaCountsVersion, Turns: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.IndexSessionPath(ctx, target, path); err != nil {
		t.Fatal(err)
	}
	record, ok, err := catalog.GetSession(ctx, path)
	if err != nil || !ok {
		t.Fatalf("GetSession: ok=%v err=%v", ok, err)
	}
	if record.LastActivityAt != current.UnixMilli() {
		t.Fatalf("lastActivityAt = %d, want preserved %d", record.LastActivityAt, current.UnixMilli())
	}
}

func TestExactIndexPreservesRootlessRecoveryRepresentative(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	root := filepath.Join(dir, "root.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	saveLineageSession(t, root, "q", "a")
	saveLineageSession(t, leaf, "q", "a", "next", "done")
	for path, meta := range map[string]agent.BranchMeta{
		root: {ID: "root", Scope: "global", TopicID: "root-topic", TopicTitle: "Root"},
		leaf: {ID: "leaf", Scope: "global", TopicID: "leaf-source-topic", TopicTitle: "Leaf", Recovered: true, ParentID: "root", RecoveryDepth: 1},
	} {
		if err := agent.SaveBranchMetaPreserveUpdated(path, meta); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(agent.BranchMetaPath(root)); err != nil {
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
	beforePage, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50})
	if err != nil || len(beforePage.Items) != 1 {
		t.Fatalf("before ListTopics: items=%+v err=%v", beforePage.Items, err)
	}
	before, ok, err := catalog.GetSession(ctx, leaf)
	if err != nil || !ok || !before.OrdinaryVisible || !before.RecoveryCanonical {
		t.Fatalf("before recovery projection: record=%+v ok=%v err=%v", before, ok, err)
	}
	if err := agent.UpdateBranchMeta(leaf, false, func(meta *agent.BranchMeta) error {
		meta.Preview = "sidecar refresh"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.IndexSessionPath(ctx, target, leaf); err != nil {
		t.Fatal(err)
	}
	afterPage, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50})
	if err != nil || len(afterPage.Items) != 1 {
		t.Fatalf("after ListTopics: items=%+v err=%v", afterPage.Items, err)
	}
	if afterPage.Items[0].RepresentativePath != beforePage.Items[0].RepresentativePath ||
		afterPage.Items[0].TopicID != beforePage.Items[0].TopicID {
		t.Fatalf("representative changed across exact index: before=%+v after=%+v", beforePage.Items[0], afterPage.Items[0])
	}
	after, ok, err := catalog.GetSession(ctx, leaf)
	if err != nil || !ok {
		t.Fatalf("after GetSession: ok=%v err=%v", ok, err)
	}
	if after.RecoveryCopy != before.RecoveryCopy || after.RecoveryGroupID != before.RecoveryGroupID ||
		after.RecoveryRole != before.RecoveryRole || after.RecoveryCanonical != before.RecoveryCanonical ||
		after.LogicalTopicID != before.LogicalTopicID || after.OrdinaryVisible != before.OrdinaryVisible ||
		after.TopicID != before.TopicID || after.TopicTitle != before.TopicTitle {
		t.Fatalf("exact index overwrote directory projection: before=%+v after=%+v", before, after)
	}
}

func TestExactUpsertSQLRetainsDirectoryProjection(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	record := SessionRecord{
		Path: "/sessions/recovery.jsonl", Directory: "/sessions", Scope: "global",
		TopicID: "projected-topic", TopicTitle: "Projected", Recovered: true, ParentID: "root",
		RecoveryCopy: true, RecoveryGroupID: "root", RecoveryRole: RecoveryRoleCoveredCopy,
		RecoveryCanonical: true, LogicalTopicID: "projected-topic", OrdinaryVisible: true,
		TurnsState: TurnsValid, Health: HealthOK,
	}
	if _, err := catalog.upsertSessionsWithNotification(ctx, []SessionRecord{record}, nil, "seed", false, upsertDirectoryProjection); err != nil {
		t.Fatal(err)
	}
	incoming := record
	incoming.TopicID = "source-topic"
	incoming.TopicTitle = "Source"
	incoming.RecoveryCopy = false
	incoming.RecoveryGroupID = "other"
	incoming.RecoveryRole = RecoveryRoleDiverged
	incoming.RecoveryCanonical = false
	incoming.LogicalTopicID = "source-topic"
	incoming.OrdinaryVisible = false
	incoming.Preview = "updated"
	if err := catalog.UpsertSession(ctx, incoming); err != nil {
		t.Fatal(err)
	}
	got, ok, err := catalog.GetSession(ctx, record.Path)
	if err != nil || !ok {
		t.Fatalf("GetSession: ok=%v err=%v", ok, err)
	}
	if got.TopicID != record.TopicID || got.TopicTitle != record.TopicTitle ||
		got.RecoveryCopy != record.RecoveryCopy || got.RecoveryGroupID != record.RecoveryGroupID ||
		got.RecoveryRole != record.RecoveryRole || got.RecoveryCanonical != record.RecoveryCanonical ||
		got.LogicalTopicID != record.LogicalTopicID || got.OrdinaryVisible != record.OrdinaryVisible {
		t.Fatalf("SQL exact-update branch replaced directory projection: got=%+v want=%+v", got, record)
	}
	if got.Preview != "updated" {
		t.Fatalf("source-owned preview = %q, want updated", got.Preview)
	}
}

func TestNewRecoveryExactIndexCreatesNoTopicShell(t *testing.T) {
	ctx := context.Background()
	catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	record := SessionRecord{
		Path: "/sessions/new-recovery.jsonl", Directory: "/sessions", Scope: "global",
		TopicID: "unproven-source-topic", TopicTitle: "Unproven", Recovered: true, ParentID: "root",
		TurnsState: TurnsValid, Health: HealthOK,
	}
	if err := catalog.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	got, ok, err := catalog.GetSession(ctx, record.Path)
	if err != nil || !ok {
		t.Fatalf("GetSession: ok=%v err=%v", ok, err)
	}
	if got.TopicID != "" || got.TopicTitle != "" || got.LogicalTopicID != "" || got.OrdinaryVisible {
		t.Fatalf("new recovery exact shell leaked a logical topic: %+v", got)
	}
	page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("new recovery exact shell appeared in ListTopics: %+v", page.Items)
	}
}
