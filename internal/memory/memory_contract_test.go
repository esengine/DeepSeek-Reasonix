package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryOldFormatLoadsWithoutWritingMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old-fact.md")
	old := "---\nname: old-fact\ntitle: Old Fact\ndescription: Existing note\ntype: project\n---\n\nbody\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	store := Store{Dir: dir}
	memories := store.List()
	if len(memories) != 1 {
		t.Fatalf("List() returned %d memories, want 1", len(memories))
	}
	if memories[0].Name != "old-fact" || memories[0].Title != "Old Fact" || memories[0].Description != "Existing note" || string(memories[0].Type) != "project" || memories[0].Body != "body" {
		t.Fatalf("old memory parsed incorrectly: %+v", memories[0])
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("List() should not migrate/write old memory files on read\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestMemoryRecallDiversifiesNearDuplicateHits(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"cache-prefix-a", "cache-prefix-b", "cache-prefix-c"} {
		body := "---\nname: " + name + "\ntitle: Cache Prefix\ndescription: cache prefix hit rate tuning\ntype: project\n---\n\ncache prefix hit rate tuning repeated note\n"
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	other := "---\nname: cache-debugging\ntitle: Cache Debugging\ndescription: cache prefix miss diagnostics\ntype: project\n---\n\ncache prefix miss diagnostics inspect request headers and provider logs\n"
	if err := os.WriteFile(filepath.Join(dir, "cache-debugging.md"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	hits, err := searchMemories(context.Background(), Store{Dir: dir}, "cache prefix hit rate", "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("diversified recall returned %d hits, want 2 distinct results: %+v", len(hits), hits)
	}
	if hits[0].Memory.Name != "cache-prefix-a" || hits[1].Memory.Name != "cache-debugging" {
		t.Fatalf("unexpected diversified order: %s, %s", hits[0].Memory.Name, hits[1].Memory.Name)
	}
	if hits[1].DiversityPenalty <= 0 {
		t.Fatalf("expected diversity explanation on second hit: %+v", hits[1])
	}
}

func TestMemoryRecallDiversityCanBeDisabled(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"cache-prefix-a", "cache-prefix-b", "cache-prefix-c"} {
		body := "---\nname: " + name + "\ntitle: Cache Prefix\ndescription: cache prefix hit rate tuning\ntype: project\n---\n\ncache prefix hit rate tuning repeated note\n"
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	off := false
	hits, err := searchMemoriesWithOptions(context.Background(), Store{Dir: dir}, "cache prefix hit rate", "", 3, RecallOptions{Diversity: &off})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Fatalf("disabled diversity returned %d hits, want 3", len(hits))
	}
}

func TestMemoryRecallStalenessRanksBelowTopHitWithoutDiversity(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	off := false
	hits := []memoryHit{
		{Memory: Memory{Name: "top", LastConfirmedAt: now}, Score: 1},
		{Memory: Memory{Name: "old", LastConfirmedAt: now.AddDate(-3, 0, 0)}, Score: 0.9},
		{Memory: Memory{Name: "recent", LastConfirmedAt: now}, Score: 0.9},
	}
	got := rerankMemoryHits(hits, 3, normalizeRecallOptions(RecallOptions{
		Diversity: &off,
		Now:       func() time.Time { return now },
	}))
	if got[0].Memory.Name != "top" || got[1].Memory.Name != "recent" || got[2].Memory.Name != "old" {
		t.Fatalf("staleness did not influence non-top ranking: %+v", got)
	}
}

func TestStoreSaveAddsAndPreservesMemoryMetadata(t *testing.T) {
	dir := t.TempDir()
	store := Store{Dir: dir}
	created := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	confirmed := created.Add(2 * time.Hour)
	path, err := store.SaveAt(Memory{Name: "durable-fact", Type: TypeProject, Description: "fact", Body: "body", SourceKind: "remember_tool"}, created)
	if err != nil {
		t.Fatal(err)
	}
	loaded, ok := loadMemory(path)
	if !ok {
		t.Fatal("saved memory did not load")
	}
	if !loaded.CreatedAt.Equal(created) || !loaded.LastConfirmedAt.Equal(created) || loaded.SourceScope != "project" || loaded.SourceKind != "remember_tool" {
		t.Fatalf("unexpected initial metadata: %+v", loaded)
	}
	if _, err := store.SaveAt(Memory{Name: "durable-fact", Type: TypeProject, Description: "updated", Body: "new body"}, confirmed); err != nil {
		t.Fatal(err)
	}
	loaded, ok = loadMemory(path)
	if !ok {
		t.Fatal("updated memory did not load")
	}
	if !loaded.CreatedAt.Equal(created) || !loaded.LastConfirmedAt.Equal(confirmed) || loaded.SourceKind != "remember_tool" {
		t.Fatalf("update did not preserve origin metadata: %+v", loaded)
	}
}

func TestArchivePreservesMemoryMetadata(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	now := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	if _, err := store.SaveAt(Memory{Name: "old-fact", Type: TypeProject, Description: "old", Body: "body", SourceKind: "user_confirmed"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Archive("old-fact"); err != nil {
		t.Fatal(err)
	}
	archived := store.ListArchived()
	if len(archived) != 1 || !archived[0].CreatedAt.Equal(now) || archived[0].SourceKind != "user_confirmed" {
		t.Fatalf("archive lost metadata: %+v", archived)
	}
}
