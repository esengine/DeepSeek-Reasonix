package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
)

func TestUnmodifiedRecoveryCopyDoesNotMigrateIntoTopics(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	parent, recovery, branchMsgs := forkDesktopRecoveryBranch(t, dir, "normal")
	coverDesktopRecoveryParent(t, parent, branchMsgs)
	metaPath := agent.BranchMetaPath(recovery)
	before, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	// Metadata discovery may proceed, but content proof must wait until this
	// test has observed both sources. Wall-clock timing cannot fence that phase.
	release, err := app.historyMaintenance.Foreground(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	app.startSessionCatalog()
	catalog := waitForSessionCatalogForTest(t, app, nil)
	catalog.PrioritizeWorkspace("global", "")
	nodes := waitForCatalogTopic(t, app, "global", "", legacySessionTopicID(parent))
	if len(nodes) != 1 || len(nodes[0].Children) != 2 {
		t.Fatalf("project tree = %#v, want both sources before content proof", nodes)
	}
	release()
	if err := catalog.ProveRecoveryLineage(t.Context()); err != nil {
		t.Fatal(err)
	}
	nodes = mustListProjectTree(t, app)
	if len(nodes) != 1 || len(nodes[0].Children) != 1 || filepath.Clean(nodes[0].Children[0].SessionPath) != filepath.Clean(parent) {
		t.Fatalf("project tree = %#v, want the covering parent after content proof", nodes)
	}
	if meta, ok, err := agent.LoadBranchMeta(recovery); err != nil || !ok {
		t.Fatalf("load recovery meta: ok=%v err=%v", ok, err)
	} else if strings.TrimSpace(meta.TopicID) != "" {
		t.Fatalf("parent-covered recovery copy was migrated into topic %q", meta.TopicID)
	}
	after, err := os.ReadFile(metaPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("content proof changed the recovery sidecar: %v", err)
	}
}
