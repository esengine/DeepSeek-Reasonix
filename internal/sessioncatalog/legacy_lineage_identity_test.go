package sessioncatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
)

func TestLegacyRecoveryIdentitySurvivesContentProofAndParentDeletion(t *testing.T) {
	for _, metadataOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "full", true: "metadata"}[metadataOnly], func(t *testing.T) {
			ctx := t.Context()
			dir := t.TempDir()
			parent, recovery, _ := forkCatalogRecoveryBranch(t, dir, "legacy-parent")
			coverCatalogRecoveryParent(t, parent, recovery)
			before := map[string]string{}
			for _, path := range []string{parent, recovery, agent.BranchMetaPath(parent), agent.BranchMetaPath(recovery)} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = string(data)
			}
			opts := Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), MetadataOnly: metadataOnly, DisableRepair: true, StartPaused: true}
			catalog, err := Open(ctx, opts)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := catalog.Close(context.Background()); err != nil {
					t.Error(err)
				}
			})
			reopen := func() {
				t.Helper()
				if err := catalog.Close(ctx); err != nil {
					t.Fatal(err)
				}
				catalog, err = Open(ctx, opts)
				if err != nil {
					t.Fatal(err)
				}
			}
			target := DirectoryTarget{Path: dir, Scope: "global"}
			check := func(path, topic string, visible bool) {
				t.Helper()
				record, ok, err := catalog.GetSession(ctx, path)
				if err != nil || !ok || record.TopicID != topic || record.OrdinaryVisible != visible {
					t.Fatalf("record = %+v, ok=%v err=%v; want topic=%q visible=%v", record, ok, err, topic, visible)
				}
			}
			reconcile := func() {
				t.Helper()
				if err := catalog.ReconcileDirectory(ctx, target); err != nil {
					t.Fatal(err)
				}
				if err := catalog.ProveRecoveryLineage(ctx); err != nil {
					t.Fatal(err)
				}
			}
			reconcile()
			check(parent, agent.LegacySessionTopicID(parent), true)
			check(recovery, agent.LegacySessionTopicID(parent), false)
			// An older executable may have certified this unchanged directory
			// with the incompatible fallback. Reopen must repair that cache too.
			oldTopic := "recovery:" + agent.BranchID(parent)
			for _, query := range []string{
				"UPDATE catalog_sessions SET topic_id=?,logical_topic_id=?",
				"UPDATE catalog_topics SET topic_id=? WHERE topic_id<>?",
			} {
				if _, err := catalog.db.ExecContext(ctx, query, oldTopic, oldTopic); err != nil {
					t.Fatal(err)
				}
			}
			reopen()
			reconcile()
			check(parent, agent.LegacySessionTopicID(parent), true)
			check(recovery, agent.LegacySessionTopicID(parent), false)
			if err := os.Remove(parent); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(agent.SessionEventLogPath(parent)); err != nil {
				t.Fatal(err)
			}
			reconcile()
			check(recovery, agent.LegacySessionTopicID(recovery), true)
			reopen()
			reconcile()
			check(recovery, agent.LegacySessionTopicID(recovery), true)
			// Reopening a rebuilt projection must converge without migrating the
			// authoritative transcript or either legacy branch sidecar.
			for _, path := range []string{recovery, agent.BranchMetaPath(parent), agent.BranchMetaPath(recovery)} {
				data, err := os.ReadFile(filepath.Clean(path))
				if err != nil || string(data) != before[path] {
					t.Fatalf("catalog changed authority %s: %v", filepath.Base(path), err)
				}
			}
		})
	}
}

func TestLegacyRootlessRecoveryGroupUsesOneRepresentativeIdentity(t *testing.T) {
	leaf := SessionRecord{Path: filepath.Join(t.TempDir(), "leaf.jsonl"), Recovered: true, ParentID: "gone", RecoveryGroupID: "gone", RecoveryCanonical: true}
	peer := SessionRecord{Path: filepath.Join(filepath.Dir(leaf.Path), "peer.jsonl"), Recovered: true, ParentID: "gone", RecoveryGroupID: "gone"}
	for _, input := range [][]SessionRecord{{leaf, peer}, {peer, leaf}} {
		visible := 0
		for _, got := range projectLogicalSessions(input) {
			if got.TopicID != agent.LegacySessionTopicID(leaf.Path) || got.LogicalTopicID != got.TopicID {
				t.Fatalf("rootless group identity = %+v", got)
			}
			if got.OrdinaryVisible {
				visible++
				if got.Path != leaf.Path {
					t.Fatalf("visible recovery = %q, want canonical leaf", got.Path)
				}
			}
		}
		if visible != 1 {
			t.Fatalf("visible recovery rows = %d, want 1", visible)
		}
	}
}

func TestExplicitRecoveryPrefixedTopicIdentityIsPreserved(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "root.jsonl")
	group := agent.BranchID(rootPath)
	oldTopic := "recovery:" + group
	root := SessionRecord{Path: rootPath}
	leaf := SessionRecord{Path: filepath.Join(filepath.Dir(rootPath), "leaf.jsonl"), Recovered: true, ParentID: group, RecoveryGroupID: group, RecoveryCanonical: true}
	for _, placement := range []string{"root", "member", "rootless"} {
		t.Run(placement, func(t *testing.T) {
			parent, recovery := root, leaf
			if placement == "root" {
				parent.TopicID, parent.TopicTitle = oldTopic, "Persisted title"
			} else {
				recovery.TopicID, recovery.TopicTitle = oldTopic, "Persisted title"
			}
			records := []SessionRecord{parent, recovery}
			if placement == "rootless" {
				records = records[1:]
			}
			for _, got := range projectLogicalSessions(records) {
				if got.TopicID != oldTopic || got.LogicalTopicID != oldTopic || got.TopicTitle != "Persisted title" {
					t.Fatalf("explicit recovery-prefixed authority changed: %+v", got)
				}
			}
		})
	}
}
