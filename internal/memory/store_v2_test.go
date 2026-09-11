package memory

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestStoreV2StableIDSurvivesRenameAndRevisionIncrements(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	first, err := store.SaveWithOptions(Memory{Name: "alpha", Description: "first", Body: "v1"}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Memory.ID == "" || first.Memory.Revision != 1 {
		t.Fatalf("first save = %+v", first.Memory)
	}

	second, err := store.SaveWithOptions(Memory{
		ID: first.Memory.ID, Name: "beta", Description: "renamed", Body: "v2",
	}, SaveOptions{ExpectedRevision: 1, RequireExpectedRevision: true})
	if err != nil {
		t.Fatal(err)
	}
	if second.Memory.ID != first.Memory.ID || second.Memory.Revision != 2 || second.Memory.Name != "beta" {
		t.Fatalf("renamed save = %+v, first = %+v", second.Memory, first.Memory)
	}
	if _, err := os.Stat(filepath.Join(store.Dir, "alpha.md")); !os.IsNotExist(err) {
		t.Fatalf("old slug path still active: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.Dir, "beta.md")); err != nil {
		t.Fatalf("renamed path missing: %v", err)
	}
}

func TestStoreV2IndexIsDerivedFromFacts(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	if _, err := store.Save(Memory{Name: "source-of-truth", Title: "Source of truth", Description: "active fact", Body: "body"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir, indexFile), []byte("# stale\n\n- [Ghost](ghost.md) — stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	index := store.Index()
	if !strings.Contains(index, "[Source of truth](project/source-of-truth.md)") || strings.Contains(index, "Ghost") {
		t.Fatalf("derived index used stale MEMORY.md:\n%s", index)
	}
}

func TestStoreV2ExpectedRevisionRejectsLostUpdate(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	first, err := store.SaveWithOptions(Memory{Name: "fact", Description: "one", Body: "v1"}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveWithOptions(Memory{ID: first.Memory.ID, Name: "fact", Description: "two", Body: "v2"}, SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveWithOptions(Memory{ID: first.Memory.ID, Name: "fact", Description: "stale", Body: "lost"}, SaveOptions{
		ExpectedRevision: 1, RequireExpectedRevision: true,
	})
	if err == nil || !strings.Contains(err.Error(), "revision conflict") {
		t.Fatalf("stale save error = %v, want revision conflict", err)
	}
	got, ok := store.Read(first.Memory.ID)
	if !ok || got.Body != "v2" || got.Revision != 2 {
		t.Fatalf("stale update changed active memory: %+v, ok=%v", got, ok)
	}
}

func TestStoreV2RestoreRevisionCreatesAuditedRevision(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	first, err := store.SaveWithOptions(Memory{Name: "fact", Description: "one", Body: "v1"}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveWithOptions(Memory{ID: first.Memory.ID, Name: "fact", Description: "two", Body: "v2"}, SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	restored, err := store.Restore(first.Memory.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Memory.ID != first.Memory.ID || restored.Memory.Revision != 3 || restored.Memory.Body != "v1" {
		t.Fatalf("restored memory = %+v", restored.Memory)
	}
	if revisions := store.Revisions(first.Memory.ID); len(revisions) < 2 || revisions[0].Revision != 2 || revisions[1].Revision != 1 {
		t.Fatalf("revision history = %+v, want revisions 2 and 1 newest first", revisions)
	}
}

func TestStoreV2LegacyMemoryGetsDeterministicIdentity(t *testing.T) {
	dir := t.TempDir()
	legacy := "---\nname: legacy-fact\ndescription: old file\nmetadata:\n  type: project\n  scope: project\n---\n\nlegacy body\n"
	if err := os.WriteFile(filepath.Join(dir, "legacy-fact.md"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	store := Store{Dir: dir}
	first := store.List()
	second := store.List()
	if len(first) != 1 || len(second) != 1 || first[0].ID == "" || first[0].ID != second[0].ID || first[0].Revision != 1 {
		t.Fatalf("legacy identities = first %+v second %+v", first, second)
	}
	if !strings.HasPrefix(first[0].ID, "legacy-") {
		t.Fatalf("legacy id = %q, want recognizable deterministic id", first[0].ID)
	}
}

func TestStoreV2MigrationPersistsLegacyIdentity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy-fact.md")
	legacy := "---\nname: legacy-fact\ndescription: old file\nmetadata:\n  type: project\n  scope: project\n---\n\nlegacy body\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	store := Store{Dir: dir}
	report, err := store.MigrateV2()
	if err != nil {
		t.Fatal(err)
	}
	if report.Migrated != 1 {
		t.Fatalf("migration report = %+v", report)
	}
	frontmatter, _ := splitFrontmatter(mustReadString(t, path))
	if !strings.HasPrefix(frontmatter["id"], "legacy-") || frontmatter["revision"] != "1" || frontmatter["created_at"] == "" || frontmatter["updated_at"] == "" {
		t.Fatalf("migrated frontmatter = %+v", frontmatter)
	}
	again, err := store.MigrateV2()
	if err != nil || again.Migrated != 0 {
		t.Fatalf("idempotent migration = %+v, %v", again, err)
	}
}

func TestStoreV2LegacyIdentityIncludesScope(t *testing.T) {
	root := t.TempDir()
	store := Store{Dir: filepath.Join(root, "project"), GlobalDir: filepath.Join(root, "global")}
	for _, fixture := range []struct {
		dir   string
		scope FactScope
	}{
		{dir: store.Dir, scope: FactScopeProject},
		{dir: store.GlobalDir, scope: FactScopeGlobal},
	} {
		if err := os.MkdirAll(fixture.dir, 0o755); err != nil {
			t.Fatal(err)
		}
		legacy := "---\nname: shared-name\ndescription: old file\nmetadata:\n  type: project\n  scope: " + string(fixture.scope) + "\n---\n\nlegacy body\n"
		if err := os.WriteFile(filepath.Join(fixture.dir, "shared-name.md"), []byte(legacy), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	project, _, ok := store.findActiveInDir(store.Dir, "shared-name")
	if !ok {
		t.Fatal("project legacy memory not found")
	}
	global, _, ok := store.findActiveInDir(store.GlobalDir, "shared-name")
	if !ok {
		t.Fatal("global legacy memory not found")
	}
	if project.ID == global.ID {
		t.Fatalf("legacy IDs collided across scopes: %q", project.ID)
	}
}

func TestStoreV2ArchiveByIDOnlyArchivesMatchingIdentity(t *testing.T) {
	root := t.TempDir()
	store := Store{Dir: filepath.Join(root, "project"), GlobalDir: filepath.Join(root, "global")}
	for _, fixture := range []struct {
		dir   string
		id    string
		scope FactScope
	}{
		{dir: store.Dir, id: "mem-project", scope: FactScopeProject},
		{dir: store.GlobalDir, id: "mem-global", scope: FactScopeGlobal},
	} {
		if err := os.MkdirAll(fixture.dir, 0o755); err != nil {
			t.Fatal(err)
		}
		memory := Memory{ID: fixture.id, Revision: 1, Name: "shared-name", Description: string(fixture.scope), Scope: fixture.scope, Body: "body"}
		if err := os.WriteFile(filepath.Join(fixture.dir, "shared-name.md"), []byte(render(memory, memory.Name)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := store.Archive("mem-project"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Dir, "shared-name.md")); !os.IsNotExist(err) {
		t.Fatalf("project memory remained active: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.GlobalDir, "shared-name.md")); err != nil {
		t.Fatalf("global memory with a different ID was archived: %v", err)
	}
}

func TestStoreV2ScopeQualifiedReferencesRoundTrip(t *testing.T) {
	root := t.TempDir()
	store := Store{Dir: filepath.Join(root, "project"), GlobalDir: filepath.Join(root, "global")}
	global, err := store.SaveWithOptions(Memory{
		Name: "global/shared.md", Description: "global", Body: "global body",
	}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.SaveWithOptions(Memory{
		Name: "project/shared.md", Description: "project", Body: "project body",
	}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if global.Memory.Name != "shared" || global.Memory.Scope != FactScopeGlobal {
		t.Fatalf("global memory = %+v", global.Memory)
	}
	if project.Memory.Name != "shared" || project.Memory.Scope != FactScopeProject {
		t.Fatalf("project memory = %+v", project.Memory)
	}
	if got, ok := store.Read("global/shared.md"); !ok || got.ID != global.Memory.ID || got.Body != "global body" {
		t.Fatalf("global qualified read = %+v, ok=%v", got, ok)
	}
	if got, ok := store.Read("project/shared.md"); !ok || got.ID != project.Memory.ID || got.Body != "project body" {
		t.Fatalf("project qualified read = %+v, ok=%v", got, ok)
	}
	if got, ok := store.Read("shared.md"); !ok || got.ID != project.Memory.ID {
		t.Fatalf("legacy unqualified read = %+v, ok=%v; want project-preferred", got, ok)
	}

	updated, err := store.SaveWithOptions(Memory{
		Name: "global/shared.md", Description: "updated global", Body: "global v2",
	}, SaveOptions{ExpectedRevision: 1, RequireExpectedRevision: true})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Memory.Name != "shared" || updated.Memory.Scope != FactScopeGlobal || updated.Memory.Revision != 2 {
		t.Fatalf("qualified update = %+v", updated.Memory)
	}
	if _, err := os.Stat(filepath.Join(store.GlobalDir, "shared.md")); err != nil {
		t.Fatalf("qualified update renamed or moved global fact: %v", err)
	}
	if got, ok := store.Read("project/shared.md"); !ok || got.ID != project.Memory.ID {
		t.Fatalf("qualified update disturbed project fact: %+v, ok=%v", got, ok)
	}
	if revisions := store.Revisions("global/shared.md"); len(revisions) != 1 || revisions[0].Revision != 1 {
		t.Fatalf("qualified revisions = %+v", revisions)
	}

	if _, err := store.Archive("project/shared.md"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Read("project/shared.md"); ok {
		t.Fatal("qualified archive left project fact active")
	}
	if got, ok := store.Read("global/shared.md"); !ok || got.ID != global.Memory.ID || got.Revision != 2 {
		t.Fatalf("qualified archive disturbed global fact: %+v, ok=%v", got, ok)
	}
}

func TestStoreV2RejectsConflictingQualifiedReferenceScope(t *testing.T) {
	root := t.TempDir()
	store := Store{Dir: filepath.Join(root, "project"), GlobalDir: filepath.Join(root, "global")}
	_, err := store.SaveWithOptions(Memory{
		Name: "global/fact.md", Scope: FactScopeProject, Description: "conflict", Body: "body",
	}, SaveOptions{})
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflicting qualified reference error = %v", err)
	}
	if len(store.ListAll()) != 0 {
		t.Fatalf("conflicting reference wrote a fact: %+v", store.ListAll())
	}
}

func TestStoreV2RestoreArchivedPreservesIdentityAndCreatesRevision(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	first, err := store.SaveWithOptions(Memory{Name: "fact", Description: "first", Body: "v1"}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	archivePath, err := store.Archive(first.Memory.ID)
	if err != nil {
		t.Fatal(err)
	}

	restored, err := store.RestoreArchived(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Memory.ID != first.Memory.ID || restored.Memory.Revision != 2 || restored.Memory.Body != "v1" {
		t.Fatalf("restored memory = %+v, first = %+v", restored.Memory, first.Memory)
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("restored archive should leave the archive list: %v", err)
	}
	revisions := store.Revisions(first.Memory.ID)
	if len(revisions) != 1 || revisions[0].Revision != 1 || revisions[0].Body != "v1" {
		t.Fatalf("revision history = %+v, want archived revision 1", revisions)
	}
}

func TestStoreV2RestoreArchivedRejectsActiveCollisions(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	first, err := store.SaveWithOptions(Memory{Name: "fact", Description: "first", Body: "archived"}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	archivePath, err := store.Archive(first.Memory.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveWithOptions(Memory{Name: "fact", Description: "replacement", Body: "active"}, SaveOptions{}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.RestoreArchived(archivePath); err == nil || !strings.Contains(err.Error(), "already active") {
		t.Fatalf("restore collision error = %v, want already active", err)
	}
	active, ok := store.Read("fact")
	if !ok || active.Body != "active" {
		t.Fatalf("restore collision overwrote active fact: %+v, ok=%v", active, ok)
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("failed restore should preserve archive: %v", err)
	}
}

func TestStoreV2RestoreArchivedIsConcurrencySafe(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	first, err := store.SaveWithOptions(Memory{Name: "fact", Description: "first", Body: "v1"}, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	archivePath, err := store.Archive(first.Memory.ID)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, restoreErr := store.RestoreArchived(archivePath)
			errs <- restoreErr
		})
	}
	wg.Wait()
	close(errs)

	var successes int
	for restoreErr := range errs {
		if restoreErr == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful restores = %d, want exactly one", successes)
	}
	active, ok := store.Read(first.Memory.ID)
	if !ok || active.Revision != 2 {
		t.Fatalf("active memory = %+v, ok=%v", active, ok)
	}
}

func TestStoreV2RestoreArchivedRejectsPathsOutsideStore(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	outside := filepath.Join(t.TempDir(), "fact.md")
	if err := os.WriteFile(outside, []byte("not an archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RestoreArchived(outside); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("outside restore error = %v, want archive not found", err)
	}
}

func TestStoreV2RestoreArchivedRejectsSymlinkEntry(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	archiveDir := filepath.Join(store.Dir, ".archive")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "fact.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(archiveDir, "fact.md")
	if err := os.Symlink(outside, archivePath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := store.RestoreArchived(archivePath); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("symlink restore error = %v, want archive not found", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside target changed: %v", err)
	}
}

func TestStoreV2RestoreArchivedRejectsSymlinkDirectory(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "fact.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	archiveDir := filepath.Join(store.Dir, ".archive")
	if err := os.Symlink(outsideDir, archiveDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	archivePath := filepath.Join(archiveDir, "fact.md")
	if _, err := store.RestoreArchived(archivePath); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("symlinked directory restore error = %v, want archive not found", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside target changed: %v", err)
	}
}
