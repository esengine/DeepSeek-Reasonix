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

// TestStoreDelete removes a fact's file and its index line while leaving others.
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
	if strings.Contains(s.Index(), "locked.md") {
		t.Fatalf("deleted read-only entry still in index:\n%s", s.Index())
	}
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
