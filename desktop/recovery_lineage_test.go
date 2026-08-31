package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncatalog"
)

func TestGetRecoveryLineageIncludesOriginalAndUserFacingMetadata(t *testing.T) {
	dir := t.TempDir()
	catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	root := filepath.Join(dir, "root.jsonl")
	branch := filepath.Join(dir, "branch.jsonl")
	save := func(path string, messages ...string) {
		t.Helper()
		session := agent.NewSession("system")
		for index, message := range messages {
			role := provider.RoleUser
			if index%2 == 1 {
				role = provider.RoleAssistant
			}
			session.Add(provider.Message{Role: role, Content: message})
		}
		if err := session.Save(path); err != nil {
			t.Fatal(err)
		}
	}
	save(root, "shared question", "shared answer", "original preview", "original answer")
	save(branch, "shared question", "shared answer", "branch preview", "branch answer")
	created := time.UnixMilli(10)
	for path, meta := range map[string]agent.BranchMeta{
		root: {
			ID: "root", Scope: "global", TopicID: "topic", TopicTitle: "Topic",
			CustomTitle: "original note", CreatedAt: created, UpdatedAt: created,
		},
		branch: {
			ID: "branch", Scope: "global", TopicID: "topic", TopicTitle: "Topic",
			CustomTitle: "branch note", CreatedAt: created.Add(time.Second), UpdatedAt: created.Add(time.Second),
			Recovered: true, ParentID: "root", RecoveryDepth: 1,
		},
	} {
		if err := agent.SaveBranchMetaPreserveUpdated(path, meta); err != nil {
			t.Fatal(err)
		}
	}
	if err := catalog.ReconcileDirectory(context.Background(), sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	app.sessionCatalog.Store(catalog)
	view := app.GetRecoveryLineage(ProjectTopicKey{Scope: "global", TopicID: "topic"})
	if len(view.Members) != 2 {
		t.Fatalf("members = %+v, want original plus recovery version", view.Members)
	}
	byPath := map[string]RecoveryLineageMember{}
	for _, member := range view.Members {
		byPath[member.Path] = member
	}
	if got := byPath[root]; got.VersionNote != "original note" || got.Preview == "" || got.CreatedAt != 10 || got.LastActivityAt == 0 {
		t.Fatalf("original metadata = %+v", got)
	}
	if got := byPath[branch]; got.VersionNote != "branch note" || got.Preview == "" || got.Turns != 2 || got.CreatedAt != 1010 {
		t.Fatalf("branch metadata = %+v", got)
	}
}

func TestGetRecoveryLineageEmptyMembersEncodeAsArray(t *testing.T) {
	view := NewApp().GetRecoveryLineage(ProjectTopicKey{Scope: "global", TopicID: "missing"})
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || string(data) == "null" || view.Members == nil {
		t.Fatalf("empty lineage must keep members as []: %s", data)
	}
}

func TestGetRecoveryLineageBindsRequestedPhysicalGroup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	catalog, err := sessioncatalog.Open(ctx, sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })

	rootA := filepath.Join(dir, "root-a.jsonl")
	copyA := filepath.Join(dir, "copy-a.jsonl")
	rootB := filepath.Join(dir, "root-b.jsonl")
	forkB := filepath.Join(dir, "fork-b.jsonl")
	save := func(path string, messages ...string) {
		t.Helper()
		session := agent.NewSession("system")
		for index, message := range messages {
			role := provider.RoleUser
			if index%2 == 1 {
				role = provider.RoleAssistant
			}
			session.Add(provider.Message{Role: role, Content: message})
		}
		if err := session.Save(path); err != nil {
			t.Fatal(err)
		}
	}
	save(rootA, "shared question", "shared answer")
	save(copyA, "shared question", "shared answer", "leaf-a extra", "leaf-a answer")
	save(rootB, "other shared question", "other shared answer", "root-b unique", "root-b answer")
	save(forkB, "other shared question", "other shared answer", "fork-b unique", "fork-b answer")
	created := time.UnixMilli(100)
	metas := map[string]agent.BranchMeta{
		rootA: {ID: "root-a", Scope: "global", TopicID: "topic", TopicTitle: "Topic", CreatedAt: created, UpdatedAt: created.Add(4 * time.Second)},
		copyA: {ID: "copy-a", Scope: "global", TopicID: "topic", TopicTitle: "Topic", Recovered: true, ParentID: "root-a", RecoveryDepth: 1, CreatedAt: created.Add(time.Second), UpdatedAt: created.Add(3 * time.Second)},
		rootB: {ID: "root-b", Scope: "global", TopicID: "topic", TopicTitle: "Topic", CreatedAt: created.Add(2 * time.Second), UpdatedAt: created.Add(2 * time.Second)},
		forkB: {ID: "fork-b", Scope: "global", TopicID: "topic", TopicTitle: "Topic", Recovered: true, ParentID: "root-b", RecoveryDepth: 1, CreatedAt: created.Add(3 * time.Second), UpdatedAt: created.Add(time.Second)},
	}
	for path, meta := range metas {
		if err := agent.SaveBranchMetaPreserveUpdated(path, meta); err != nil {
			t.Fatal(err)
		}
	}
	if err := catalog.ReconcileDirectory(ctx, sessioncatalog.DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{}, detachedSessions: map[string]*WorkspaceTab{}}
	app.sessionCatalog.Store(catalog)

	diverged := app.GetRecoveryLineage(ProjectTopicKey{Scope: "global", TopicID: "topic", Path: forkB})
	if diverged.GroupID != "root-b" || diverged.State != "diverged" || diverged.Unresolved != 1 || len(diverged.Members) != 2 {
		t.Fatalf("diverged lineage = %+v, want only root-b and fork-b", diverged)
	}
	for _, member := range diverged.Members {
		if member.Path != rootB && member.Path != forkB {
			t.Fatalf("diverged lineage contains unrelated member %+v", member)
		}
	}

	adopted := app.GetRecoveryLineage(ProjectTopicKey{Scope: "global", TopicID: "topic", Path: copyA})
	if adopted.GroupID != "root-a" || adopted.State != "adopted" || len(adopted.Members) != 2 {
		t.Fatalf("adopted lineage = %+v, want only root-a and copy-a", adopted)
	}

	ambiguous := app.GetRecoveryLineage(ProjectTopicKey{Scope: "global", TopicID: "topic"})
	if ambiguous.GroupID != "" || len(ambiguous.Members) != 0 || ambiguous.Members == nil {
		t.Fatalf("ambiguous legacy lookup = %+v, want safe empty array", ambiguous)
	}
}
