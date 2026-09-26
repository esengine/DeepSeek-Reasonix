package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncatalog"
)

// A 1.38 write conflict leaves one recovery copy per writer, each listed as
// its own row of the same conversation. Archiving any of them archives them
// all; archiving one alone brought the next copy up in its place.
func TestArchivingRecoveredLegacyRowArchivesEveryCopy(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	parent := filepath.Join(dir, "20260826-101500.000000000-fake-model.jsonl")
	p := agent.NewSession("system")
	p.Add(provider.Message{ID: "q0", Role: provider.RoleUser, Content: "shops from my search"})
	if err := p.Save(parent); err != nil {
		t.Fatal(err)
	}
	meta := agent.BranchMeta{Scope: "global", TopicID: "topic_jd", TopicTitle: "shops from my search"}
	for _, answer := range []string{"first writer", "second writer", "third writer"} {
		s := agent.NewSession("system")
		s.Add(provider.Message{ID: "q0", Role: provider.RoleUser, Content: "shops from my search"})
		s.Add(provider.Message{ID: "a-" + answer, Role: provider.RoleAssistant, Content: answer})
		if _, err := s.SaveConflictRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: parent, Reason: "conflict", BranchMeta: meta}); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "20260826-101500.000000000-fake-model.") {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	// An unrelated recovered conversation must survive.
	other := filepath.Join(dir, "20260826-120000.000000000-fake-model.jsonl")
	o := agent.NewSession("system")
	o.Add(provider.Message{ID: "q0", Role: provider.RoleUser, Content: "another conversation"})
	if err := o.Save(other); err != nil {
		t.Fatal(err)
	}
	if _, err := o.SaveConflictRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: other, Reason: "conflict",
		BranchMeta: agent.BranchMeta{Scope: "global", TopicID: "topic_other", TopicTitle: "another conversation"}}); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.ctx = t.Context()
	pinDesktopSessionRoot(t, app)
	installNoopRuntimeEvents(app)
	t.Cleanup(app.closeSessionServices)
	catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true, MetadataOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	app.sessionCatalog.Store(catalog)
	t.Cleanup(func() { app.stopSessionCatalog(time.Second) })
	recoveredRows := func(topic string) []ProjectNode {
		t.Helper()
		if err := catalog.ReconcileDirectory(context.Background(), sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
			t.Fatal(err)
		}
		page, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "global", Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		app.ReleaseReadSnapshot(page.SnapshotID)
		var rows []ProjectNode
		for _, n := range page.Items {
			if n.Recovered && n.TopicID == topic {
				rows = append(rows, n)
			}
		}
		return rows
	}
	rows := recoveredRows("topic_jd")
	if len(rows) != 3 {
		t.Fatalf("recovered rows before archive = %d, want the 3 copies", len(rows))
	}
	target := rows[0]
	if _, err := app.ArchiveSessionTarget(SessionSelector{Ref: target.Session, Source: target.Source, SessionPath: target.SessionPath}); err != nil {
		t.Fatal(err)
	}
	if left := recoveredRows("topic_jd"); len(left) != 0 {
		t.Fatalf("archiving one recovered row left %d copies of the same conversation listed", len(left))
	}
	if kept := recoveredRows("topic_other"); len(kept) != 1 {
		t.Fatalf("an unrelated recovered conversation was archived too: %d rows left, want 1", len(kept))
	}
}
