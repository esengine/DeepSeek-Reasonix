package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStoreSaveAndIndex covers the round-trip: Save writes a frontmatter file,
// reindex adds exactly one index line, and List parses it back.
func TestStoreSaveAndIndex(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: filepath.Join(dir, "memory")}

	path, err := s.Save(Memory{
		Name:        "Prefers Tabs",
		Description: "User prefers tabs over spaces",
		Type:        TypeUser,
		Body:        "Always indent with tabs in this project.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "prefers-tabs.md" {
		t.Fatalf("name not slugified into filename: %s", path)
	}

	idx := s.Index()
	if !strings.Contains(idx, "prefers-tabs.md") || !strings.Contains(idx, "User prefers tabs") {
		t.Fatalf("index missing entry:\n%s", idx)
	}

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("want 1 memory, got %d", len(list))
	}
	m := list[0]
	if m.Name != "prefers-tabs" || m.Type != TypeUser {
		t.Fatalf("round-trip mismatch: %+v", m)
	}
	if !strings.Contains(m.Body, "indent with tabs") {
		t.Fatalf("body not preserved: %q", m.Body)
	}
}

// TestStoreOverwriteDoesNotDuplicateIndex verifies re-saving the same name
// replaces its index line rather than appending a second.
func TestStoreOverwriteDoesNotDuplicateIndex(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, desc := range []string{"first version", "second version"} {
		if _, err := s.Save(Memory{Name: "note", Description: desc, Type: TypeProject, Body: "b"}); err != nil {
			t.Fatal(err)
		}
	}
	idx := s.Index()
	if n := strings.Count(idx, "note.md"); n != 1 {
		t.Fatalf("want exactly 1 index line for note, got %d:\n%s", n, idx)
	}
	if !strings.Contains(idx, "second version") || strings.Contains(idx, "first version") {
		t.Fatalf("index not updated to latest description:\n%s", idx)
	}
}

// TestStoreIndexPreservesHandEdits verifies reindex keeps unrelated lines, so a
// user hand-editing MEMORY.md isn't clobbered when the model saves a new fact.
func TestStoreIndexPreservesHandEdits(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Save(Memory{Name: "alpha", Description: "first", Type: TypeProject, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(Memory{Name: "beta", Description: "second", Type: TypeProject, Body: "y"}); err != nil {
		t.Fatal(err)
	}
	idx := s.Index()
	if !strings.Contains(idx, "alpha.md") || !strings.Contains(idx, "beta.md") {
		t.Fatalf("an entry was lost on the second save:\n%s", idx)
	}
}

// TestStoreSaveTitleInIndexAndFrontmatter verifies an explicit title becomes the
// index link label and round-trips through the file's frontmatter.
func TestStoreSaveTitleInIndexAndFrontmatter(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Save(Memory{
		Name:        "tabs-rule",
		Title:       "Prefers tabs",
		Description: "indent with tabs",
		Type:        TypeUser,
		Body:        "b",
	}); err != nil {
		t.Fatal(err)
	}
	if idx := s.Index(); !strings.Contains(idx, "[Prefers tabs](tabs-rule.md)") {
		t.Fatalf("index link should use the title label:\n%s", idx)
	}
	if got := s.List()[0].Title; got != "Prefers tabs" {
		t.Fatalf("title not round-tripped: %q", got)
	}
}

// TestStoreIndexLabelFallsBackToDeKebabbedName checks a title-less memory still
// gets a readable label instead of a bare slug.
func TestStoreIndexLabelFallsBackToDeKebabbedName(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Save(Memory{Name: "likes-go", Description: "d", Type: TypeUser, Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if idx := s.Index(); !strings.Contains(idx, "[likes go](likes-go.md)") {
		t.Fatalf("missing-title label should de-kebab the name:\n%s", idx)
	}
}

// TestStoreDelete archives a fact's file and removes its index line while
// leaving others.
func TestStoreDelete(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, n := range []string{"alpha", "beta"} {
		if _, err := s.Save(Memory{Name: n, Description: "d", Type: TypeProject, Body: "b"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Delete("alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "alpha.md")); !os.IsNotExist(err) {
		t.Fatalf("alpha.md should be gone, stat err = %v", err)
	}
	archived := archivedFiles(t, s.Dir)
	if len(archived) != 1 || !strings.HasSuffix(archived[0], "-alpha.md") {
		t.Fatalf("archive files = %v, want one alpha archive", archived)
	}
	idx := s.Index()
	if strings.Contains(idx, "alpha.md") {
		t.Fatalf("deleted entry still in index:\n%s", idx)
	}
	if !strings.Contains(idx, "beta.md") {
		t.Fatalf("unrelated entry lost on delete:\n%s", idx)
	}
	if names := s.List(); len(names) != 1 || names[0].Name != "beta" {
		t.Fatalf("List after delete = %+v, want only beta", names)
	}
}

// TestStoreDeleteMissingIsNoError treats deleting an absent memory as success —
// the goal state (gone) already holds.
func TestStoreDeleteMissingIsNoError(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if err := s.Delete("never-saved"); err != nil {
		t.Fatalf("deleting a missing memory should not error: %v", err)
	}
}

func TestSafeJoinRejectsStoreEscape(t *testing.T) {
	dir := t.TempDir()
	if _, err := safeJoin(dir, filepath.Join("..", "outside.md")); err == nil {
		t.Fatal("safeJoin should reject paths outside the store")
	}
	if _, err := safeJoin(dir, filepath.Join(t.TempDir(), "outside.md")); err == nil {
		t.Fatal("safeJoin should reject absolute paths outside the store")
	}
}

func TestStoreArchiveSanitizesNameBeforePathUse(t *testing.T) {
	root := t.TempDir()
	s := Store{Dir: filepath.Join(root, "memory")}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.md")
	if err := os.WriteFile(outside, []byte("do not move"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Archive("../outside"); err != nil {
		t.Fatalf("Archive with path-like name should be treated as a slug, not a path: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file should remain untouched: %v", err)
	}
}

func TestStoreDeleteRepairsReadOnlyMemoryFile(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Save(Memory{Name: "locked", Description: "d", Type: TypeProject, Body: "b"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Dir, "locked.md")
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("locked"); err != nil {
		t.Fatalf("delete read-only memory: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("locked.md should be gone, stat err = %v", err)
	}
	archived := archivedFiles(t, s.Dir)
	if len(archived) != 1 || !strings.HasSuffix(archived[0], "-locked.md") {
		t.Fatalf("archive files = %v, want one locked archive", archived)
	}
	if strings.Contains(s.Index(), "locked.md") {
		t.Fatalf("deleted read-only entry still in index:\n%s", s.Index())
	}
}

func TestStoreArchiveReturnsArchivePath(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.Save(Memory{Name: "old-fact", Description: "d", Type: TypeProject, Body: "body"}); err != nil {
		t.Fatal(err)
	}
	archive, err := s.Archive("old-fact")
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if archive == "" {
		t.Fatal("Archive returned empty path for existing memory")
	}
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if !strings.Contains(string(body), "body") {
		t.Fatalf("archive missing memory body:\n%s", body)
	}
	if strings.Contains(s.Index(), "old-fact.md") {
		t.Fatalf("archived memory still in index:\n%s", s.Index())
	}
	archived := s.ListArchived()
	if len(archived) != 1 {
		t.Fatalf("ListArchived = %+v, want one entry", archived)
	}
	if archived[0].Name != "old-fact" || archived[0].Path != archive {
		t.Fatalf("archived entry mismatch: %+v, path %q", archived[0], archive)
	}
	if archived[0].ArchivedAt.IsZero() {
		t.Fatalf("archived entry missing timestamp: %+v", archived[0])
	}
	if len(s.List()) != 0 {
		t.Fatalf("active List should exclude archived memories: %+v", s.List())
	}
}

func TestStoreListArchivedNewestFirst(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	dir := filepath.Join(s.Dir, ".archive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := []struct {
		name string
		body string
	}{
		{"20260101-010000.000-old.md", render(Memory{Name: "old", Description: "old d", Type: TypeProject, Body: "old body"}, "old")},
		{"20260102-010000.000-new.md", render(Memory{Name: "new", Description: "new d", Type: TypeFeedback, Body: "new body"}, "new")},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archived := s.ListArchived()
	if len(archived) != 2 {
		t.Fatalf("ListArchived len = %d, want 2: %+v", len(archived), archived)
	}
	if archived[0].Name != "new" || archived[1].Name != "old" {
		t.Fatalf("ListArchived order = %+v, want newest first", archived)
	}
	if archived[0].Type != TypeFeedback || !strings.Contains(archived[1].Body, "old body") {
		t.Fatalf("archived memory did not round-trip metadata/body: %+v", archived)
	}
}

func archivedFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, ".archive"))
	if err != nil {
		t.Fatalf("read archive dir: %v", err)
	}
	var out []string
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out
}

// TestNormalizeType maps unknown types to project and keeps known ones.
func TestNormalizeType(t *testing.T) {
	if got := NormalizeType("feedback"); got != TypeFeedback {
		t.Errorf("feedback: got %q", got)
	}
	if got := NormalizeType("garbage"); got != TypeProject {
		t.Errorf("unknown should default to project, got %q", got)
	}
}

// TestStoreForSlug ensures the project path becomes one filesystem-safe segment.
func TestStoreForSlug(t *testing.T) {
	s := StoreFor("/home/me/.config/reasonix", "/Users/me/proj")
	if strings.Count(filepath.Base(filepath.Dir(s.Dir)), "/") != 0 {
		t.Fatalf("slug should have no separators: %s", s.Dir)
	}
	if !strings.Contains(s.Dir, "-Users-me-proj") {
		t.Fatalf("unexpected slug: %s", s.Dir)
	}
}

// TestDisabledStoreIsNoOp ensures a zero Store (no user config dir) never panics
// and errors cleanly on Save.
func TestDisabledStoreIsNoOp(t *testing.T) {
	var s Store
	if s.Index() != "" || s.List() != nil {
		t.Fatal("disabled store should read empty")
	}
	if _, err := s.Save(Memory{Name: "x", Description: "d", Body: "b"}); err == nil {
		t.Fatal("disabled store Save should error, not silently drop")
	}
}

// TestStoreGlobalAndProject verifies that TypeUser/TypeFeedback memories are
// routed to GlobalDir, TypeProject/TypeReference stay in Dir, List() merges
// both, and Delete() removes from the correct directory.
func TestStoreGlobalAndProject(t *testing.T) {
	dir := t.TempDir()
	s := Store{
		Dir:       filepath.Join(dir, "project", "memory"),
		GlobalDir: filepath.Join(dir, "global"),
	}

	// TypeUser → GlobalDir
	pUser, err := s.Save(Memory{Name: "prefers-tabs", Description: "user pref", Type: TypeUser, Body: "use tabs"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pUser, s.GlobalDir) {
		t.Fatalf("TypeUser should go to GlobalDir, got %s", pUser)
	}

	// TypeProject → Dir
	pProj, err := s.Save(Memory{Name: "build-target", Description: "build target", Type: TypeProject, Body: "go build"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pProj, s.Dir) {
		t.Fatalf("TypeProject should go to Dir, got %s", pProj)
	}

	// TypeFeedback → GlobalDir
	pFb, err := s.Save(Memory{Name: "no-emoji", Description: "no emoji", Type: TypeFeedback, Body: "skip emoji"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pFb, s.GlobalDir) {
		t.Fatalf("TypeFeedback should go to GlobalDir, got %s", pFb)
	}

	// TypeReference → Dir
	pRef, err := s.Save(Memory{Name: "api-docs", Description: "api docs", Type: TypeReference, Body: "see docs"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pRef, s.Dir) {
		t.Fatalf("TypeReference should go to Dir, got %s", pRef)
	}

	// List merges both directories
	list := s.List()
	if len(list) != 4 {
		t.Fatalf("want 4 memories, got %d", len(list))
	}

	// Index merges both directories
	idx := s.Index()
	if !strings.Contains(idx, "prefers-tabs") || !strings.Contains(idx, "build-target") {
		t.Fatalf("index should contain both global and project memories:\n%s", idx)
	}

	// Delete removes from the correct directory
	if err := s.Delete("prefers-tabs"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pUser); !os.IsNotExist(err) {
		t.Fatal("global memory file should be gone after delete")
	}

	// List after delete
	list2 := s.List()
	if len(list2) != 3 {
		t.Fatalf("want 3 memories after delete, got %d", len(list2))
	}

	// Index should not duplicate # Memory headers (Block() adds its own).
	idx2 := s.Index()
	if strings.Count(idx2, "# Memory") != 0 {
		t.Fatalf("Index should have 0 # Memory headers (Block() adds one), got %d:\n%s", strings.Count(idx2, "# Memory"), idx2)
	}
}

// TestStoreForInitializesGlobalDir ensures StoreFor sets GlobalDir alongside Dir.
func TestStoreForInitializesGlobalDir(t *testing.T) {
	s := StoreFor("/home/me/.config/reasonix", "/Users/me/proj")
	if s.GlobalDir == "" {
		t.Fatal("StoreFor should set GlobalDir")
	}
	if !strings.Contains(s.GlobalDir, "memory") || !strings.Contains(s.GlobalDir, "global") {
		t.Fatalf("unexpected GlobalDir: %s", s.GlobalDir)
	}
	if s.GlobalDir == s.Dir {
		t.Fatal("GlobalDir and Dir should be different paths")
	}
}

// TestDirForRoutesCorrectly verifies DirFor routes user/feedback to GlobalDir
// and everything else to Dir.
func TestDirForRoutesCorrectly(t *testing.T) {
	dir := t.TempDir()
	s := Store{
		Dir:       filepath.Join(dir, "project", "memory"),
		GlobalDir: filepath.Join(dir, "global"),
	}
	if got := s.DirFor(TypeUser); got != s.GlobalDir {
		t.Errorf("TypeUser: got %q, want %q", got, s.GlobalDir)
	}
	if got := s.DirFor(TypeFeedback); got != s.GlobalDir {
		t.Errorf("TypeFeedback: got %q, want %q", got, s.GlobalDir)
	}
	if got := s.DirFor(TypeProject); got != s.Dir {
		t.Errorf("TypeProject: got %q, want %q", got, s.Dir)
	}
	if got := s.DirFor(TypeReference); got != s.Dir {
		t.Errorf("TypeReference: got %q, want %q", got, s.Dir)
	}
}

// TestDirForFallsBackWhenNoGlobalDir ensures DirFor falls back to Dir when
// GlobalDir is empty.
func TestDirForFallsBackWhenNoGlobalDir(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: filepath.Join(dir, "memory")}
	if got := s.DirFor(TypeUser); got != s.Dir {
		t.Errorf("TypeUser without GlobalDir should fall back to Dir, got %q", got)
	}
}

// TestStoreDeleteRemovesFromAllDirs verifies that after a type-routing migration
// (same name in both GlobalDir and Dir), Delete removes both copies so the
// memory truly disappears.
func TestStoreDeleteRemovesFromAllDirs(t *testing.T) {
	dir := t.TempDir()
	s := Store{
		Dir:       filepath.Join(dir, "project", "memory"),
		GlobalDir: filepath.Join(dir, "global"),
	}

	// Simulate migration: write a TypeUser memory directly into both dirs.
	name := "prefers-tabs"
	for _, d := range []string{s.Dir, s.GlobalDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		m := Memory{Name: name, Description: "user pref", Type: TypeUser, Body: "use tabs"}
		if err := os.WriteFile(filepath.Join(d, name+".md"), []byte(render(m, name)), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := reindexIn(d, name, m); err != nil {
			t.Fatal(err)
		}
	}

	// Both copies should appear, but deduplicated.
	list := s.List()
	if len(list) != 1 {
		t.Fatalf("want 1 deduplicated memory, got %d", len(list))
	}

	// Delete should remove from BOTH directories.
	if err := s.Delete(name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.GlobalDir, name+".md")); !os.IsNotExist(err) {
		t.Fatal("global copy should be gone after delete")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, name+".md")); !os.IsNotExist(err) {
		t.Fatal("project copy should be gone after delete")
	}

	list2 := s.List()
	if len(list2) != 0 {
		t.Fatalf("want 0 memories after delete, got %d", len(list2))
	}
	if idx := s.Index(); idx != "" {
		t.Fatalf("Index() should be empty after deleting all entries, got:\n%s", idx)
	}
}

// TestStoreIndexDeduplicatesAcrossDirs verifies Index() does not emit duplicate
// lines when the same memory name exists in both GlobalDir and Dir.
func TestStoreIndexDeduplicatesAcrossDirs(t *testing.T) {
	dir := t.TempDir()
	s := Store{
		Dir:       filepath.Join(dir, "project", "memory"),
		GlobalDir: filepath.Join(dir, "global"),
	}

	// Write the same memory into both dirs (migration scenario).
	name := "prefers-tabs"
	for _, d := range []string{s.GlobalDir, s.Dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		m := Memory{Name: name, Description: "user pref", Type: TypeUser, Body: "use tabs"}
		if err := os.WriteFile(filepath.Join(d, name+".md"), []byte(render(m, name)), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := reindexIn(d, name, m); err != nil {
			t.Fatal(err)
		}
	}

	idx := s.Index()
	count := strings.Count(idx, name+".md")
	if count != 1 {
		t.Fatalf("want exactly 1 index line for %s, got %d:\n%s", name, count, idx)
	}
	if strings.Count(idx, "# Memory") != 0 {
		t.Fatalf("merged index should have 0 # Memory headers (Block() adds one), got %d:\n%s", strings.Count(idx, "# Memory"), idx)
	}
}

// TestStoreSaveVerifiesIndexDir verifies that Save writes the MEMORY.md
// index to the correct directory for the memory type.
func TestStoreSaveVerifiesIndexDir(t *testing.T) {
	dir := t.TempDir()
	s := Store{
		Dir:       filepath.Join(dir, "project", "memory"),
		GlobalDir: filepath.Join(dir, "global"),
	}

	// TypeUser → GlobalDir
	if _, err := s.Save(Memory{Name: "user-pref", Description: "d", Type: TypeUser, Body: "b"}); err != nil {
		t.Fatal(err)
	}
	gb, _ := os.ReadFile(filepath.Join(s.GlobalDir, indexFile))
	pb, _ := os.ReadFile(filepath.Join(s.Dir, indexFile))
	if !strings.Contains(string(gb), "user-pref") {
		t.Fatal("GlobalDir MEMORY.md should contain user-pref")
	}
	if strings.Contains(string(pb), "user-pref") {
		t.Fatal("Dir MEMORY.md should NOT contain user-pref (it went to GlobalDir)")
	}

	// TypeProject → Dir
	if _, err := s.Save(Memory{Name: "build-cmd", Description: "d", Type: TypeProject, Body: "b"}); err != nil {
		t.Fatal(err)
	}
	pb2, _ := os.ReadFile(filepath.Join(s.Dir, indexFile))
	if !strings.Contains(string(pb2), "build-cmd") {
		t.Fatal("Dir MEMORY.md should contain build-cmd")
	}
	gb2, _ := os.ReadFile(filepath.Join(s.GlobalDir, indexFile))
	if strings.Contains(string(gb2), "build-cmd") {
		t.Fatal("GlobalDir MEMORY.md should NOT contain build-cmd (it went to Dir)")
	}
}

// TestStoreDeleteFlushesIndexPerDir verifies that Delete calls flushIndexIn
// for each directory where the memory file existed.
func TestStoreDeleteFlushesIndexPerDir(t *testing.T) {
	dir := t.TempDir()
	s := Store{
		Dir:       filepath.Join(dir, "project", "memory"),
		GlobalDir: filepath.Join(dir, "global"),
	}

	// Write to both dirs manually (migration scenario).
	name := "prefers-tabs"
	for _, d := range []string{s.GlobalDir, s.Dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		m := Memory{Name: name, Description: "d", Type: TypeUser, Body: "b"}
		if err := os.WriteFile(filepath.Join(d, name+".md"), []byte(render(m, name)), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := reindexIn(d, name, m); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.Delete(name); err != nil {
		t.Fatal(err)
	}

	// Verify both MEMORY.md files have the entry removed.
	gb, _ := os.ReadFile(filepath.Join(s.GlobalDir, indexFile))
	pb, _ := os.ReadFile(filepath.Join(s.Dir, indexFile))
	if strings.Contains(string(gb), name+".md") {
		t.Fatalf("GlobalDir MEMORY.md should not reference %s after delete:\n%s", name, gb)
	}
	if strings.Contains(string(pb), name+".md") {
		t.Fatalf("Dir MEMORY.md should not reference %s after delete:\n%s", name, pb)
	}

	// Index() should return "" (no entries, no orphaned header).
	idx := s.Index()
	if idx != "" {
		t.Fatalf("Index() should return empty after deleting all entries, got:\n%s", idx)
	}
}

// TestStorePathWithGlobalDir verifies Path() checks GlobalDir first and
// falls back to Dir for new files.
func TestStorePathWithGlobalDir(t *testing.T) {
	dir := t.TempDir()
	s := Store{
		Dir:       filepath.Join(dir, "project", "memory"),
		GlobalDir: filepath.Join(dir, "global"),
	}

	// No files yet → defaults to Dir.
	p := s.Path("new-fact")
	if !strings.HasPrefix(p, s.Dir) {
		t.Fatalf("Path for new file should default to Dir, got %s", p)
	}

	// Write a file to GlobalDir.
	if err := os.MkdirAll(s.GlobalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.GlobalDir, "existing.md"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	p2 := s.Path("existing")
	if !strings.HasPrefix(p2, s.GlobalDir) {
		t.Fatalf("Path for file in GlobalDir should return GlobalDir path, got %s", p2)
	}

	// Write a file to Dir (not GlobalDir).
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "proj-fact.md"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	p3 := s.Path("proj-fact")
	if !strings.HasPrefix(p3, s.Dir) {
		t.Fatalf("Path for file only in Dir should return Dir path, got %s", p3)
	}
}
