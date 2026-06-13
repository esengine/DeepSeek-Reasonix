package fileref

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func mkfile(t *testing.T, path string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "main.go"))

	result := Search(dir, "", 10)
	if result != nil {
		t.Fatalf("expected nil for empty query, got %v", result)
	}
}

func TestSearchShortQuery(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "main.go"))

	result := Search(dir, "x", 10)
	if result != nil {
		t.Fatalf("expected nil for single-char query, got %v", result)
	}
}

func TestSearchQueryWithSlash(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "main.go"))

	result := Search(dir, "a/b", 10)
	if result != nil {
		t.Fatalf("expected nil for query with slash, got %v", result)
	}

	result = Search(dir, "a\\b", 10)
	if result != nil {
		t.Fatalf("expected nil for query with backslash, got %v", result)
	}
}

func TestSearchNonPositiveLimit(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "main.go"))

	result := Search(dir, "main", 0)
	if result != nil {
		t.Fatalf("expected nil for zero limit, got %v", result)
	}

	result = Search(dir, "main", -1)
	if result != nil {
		t.Fatalf("expected nil for negative limit, got %v", result)
	}
}

func TestSearchBasic(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "main.go"))
	mkfile(t, filepath.Join(dir, "util.go"))
	mkfile(t, filepath.Join(dir, "README.md"))

	result := Search(dir, "go", 10)
	if len(result) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(result), result)
	}
	sort.Strings(result)
	if result[0] != "main.go" || result[1] != "util.go" {
		t.Fatalf("expected ['main.go', 'util.go'], got %v", result)
	}
}

func TestSearchCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "MAIN.GO"))
	mkfile(t, filepath.Join(dir, "Main.js"))

	result := Search(dir, "main", 10)
	if len(result) != 2 {
		t.Fatalf("expected 2 case-insensitive matches, got %d: %v", len(result), result)
	}
}

func TestSearchLimit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		mkfile(t, filepath.Join(dir, "file_a_", "test_%d.go", "dummy.txt"))
	}
	for i := 0; i < 10; i++ {
		mkfile(t, filepath.Join(dir, "file_%d.txt", "dummy.txt"))
	}
	for i := 0; i < 5; i++ {
		mkfile(t, filepath.Join(dir, "target_%d.txt", "dummy.txt"))
	}

	result := Search(dir, "target", 3)
	if len(result) > 3 {
		t.Fatalf("expected at most 3 results, got %d: %v", len(result), result)
	}
}

func TestSearchNoMatch(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "main.go"))

	result := Search(dir, "nonexistent", 10)
	if len(result) != 0 {
		t.Fatalf("expected 0 results, got %d: %v", len(result), result)
	}
}

func TestSearchSkipsDotGit(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, ".git", "objects", "abc123"))
	mkfile(t, filepath.Join(dir, "myfile.go"))

	result := Search(dir, "abc", 10)
	// .git contents should be skipped
	if len(result) != 0 {
		t.Fatalf("expected 0 results (skipped .git), got %d: %v", len(result), result)
	}
}

func TestSearchSkipsNodeModules(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "node_modules", "lodash", "index.js"))
	mkfile(t, filepath.Join(dir, "src", "app.js"))

	result := Search(dir, "index", 10)
	if len(result) != 0 {
		t.Fatalf("expected 0 results (skipped node_modules), got %d: %v", len(result), result)
	}
}

func TestSearchSkipsHiddenFiles(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, ".secret.txt"))
	mkfile(t, filepath.Join(dir, "visible.txt"))

	result := Search(dir, "txt", 10)
	if len(result) != 1 {
		t.Fatalf("expected 1 visible file, got %d: %v", len(result), result)
	}
	if result[0] != "visible.txt" {
		t.Fatalf("expected 'visible.txt', got %q", result[0])
	}
}

func TestSearchShowsHiddenWithDotQuery(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, ".secret.txt"))
	mkfile(t, filepath.Join(dir, "other.txt"))

	result := Search(dir, ".secret", 10)
	if len(result) != 1 {
		t.Fatalf("expected 1 match for .secret, got %d: %v", len(result), result)
	}
	if result[0] != ".secret.txt" {
		t.Fatalf("expected '.secret.txt', got %q", result[0])
	}
}

func TestSearchSkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, ".config", "settings.json"))
	mkfile(t, filepath.Join(dir, "main.go"))

	result := Search(dir, "json", 10)
	if len(result) != 0 {
		t.Fatalf("expected 0 results (hidden dir skipped), got %d: %v", len(result), result)
	}
}

func TestSearchSkipsDSStore(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, ".DS_Store"))
	mkfile(t, filepath.Join(dir, "real.txt"))

	result := Search(dir, "store", 10)
	if len(result) != 0 {
		t.Fatalf("expected 0 results (DS_Store skipped), got %d: %v", len(result), result)
	}
}

func TestSearchSkipsSkipDirNames(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "build", "output.o"))
	mkfile(t, filepath.Join(dir, "dist", "bundle.js"))
	mkfile(t, filepath.Join(dir, "src", "main.go"))

	result := Search(dir, "output", 10)
	if len(result) != 0 {
		t.Fatalf("expected 0 results (build dir skipped), got %d: %v", len(result), result)
	}

	result = Search(dir, "bundle", 10)
	if len(result) != 0 {
		t.Fatalf("expected 0 results (dist dir skipped), got %d: %v", len(result), result)
	}
}

func TestSearchSkipsSkipDirPaths(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "bin", "tool"))
	mkfile(t, filepath.Join(dir, "tmp", "scratch"))
	mkfile(t, filepath.Join(dir, "stage", "artifact"))

	result := Search(dir, "tool", 10)
	if len(result) != 0 {
		t.Fatalf("expected 0 results (bin skipped), got %d: %v", len(result), result)
	}

	result = Search(dir, "tmp", 10)
	// "tmp" is in skipDirPaths but it's also a dir entry name — handled
	if len(result) != 0 {
		t.Fatalf("expected 0 results (tmp skipped), got %d: %v", len(result), result)
	}
}

func TestSearchReturnsRelativePaths(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "a", "b", "c", "target.go"))

	result := Search(dir, "target", 10)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(result), result)
	}
	want := "a/b/c/target.go"
	if result[0] != want {
		t.Fatalf("expected %q, got %q", want, result[0])
	}
}

func TestSearchSorted(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "z.txt"))
	mkfile(t, filepath.Join(dir, "a.txt"))
	mkfile(t, filepath.Join(dir, "m.txt"))

	result := Search(dir, "txt", 10)
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d: %v", len(result), result)
	}
	if result[0] != "a.txt" || result[1] != "m.txt" || result[2] != "z.txt" {
		t.Fatalf("expected sorted ['a.txt', 'm.txt', 'z.txt'], got %v", result)
	}
}

func TestSearchEmptyDir(t *testing.T) {
	dir := t.TempDir()
	result := Search(dir, "go", 10)
	if len(result) != 0 {
		t.Fatalf("expected 0 results in empty dir, got %d: %v", len(result), result)
	}
}

func TestSearchSkipsThumbsDb(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "Thumbs.db"))
	mkfile(t, filepath.Join(dir, "photo.jpg"))

	result := Search(dir, "thumbs", 10)
	if len(result) != 0 {
		t.Fatalf("expected 0 results (Thumbs.db skipped), got %d: %v", len(result), result)
	}
}

func TestSearchNestedFiles(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "src", "main.go"))
	mkfile(t, filepath.Join(dir, "src", "util", "helper.go"))
	mkfile(t, filepath.Join(dir, "test", "main_test.go"))

	result := Search(dir, "main", 10)
	if len(result) != 2 {
		t.Fatalf("expected 2 matches for 'main', got %d: %v", len(result), result)
	}
}

func TestSearchWalkBound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping walk bound test in short mode")
	}

	dir := t.TempDir()
	// Create more entries than maxWalkEntries
	for i := 0; i < maxWalkEntries+100; i++ {
		sub := filepath.Join(dir, "dir_%d", "nested_%d", "dummy.txt", "extra.txt")
		mkfile(t, filepath.Join(dir, sub, "file.txt"))
	}

	result := Search(dir, "file", 1000)
	if len(result) > maxWalkEntries {
		t.Fatalf("expected at most %d results (walk bound), got %d", maxWalkEntries, len(result))
	}
}

func TestSearchDirEntrySkips(t *testing.T) {
	dir := t.TempDir()
	// .codex and .codegraph dirs should be skipped
	mkfile(t, filepath.Join(dir, ".codex", "index.dat"))
	mkfile(t, filepath.Join(dir, ".codegraph", "graph.bin"))
	// .pnpm-store should be skipped
	mkfile(t, filepath.Join(dir, ".pnpm-store", "package.json"))
	// .npm dir should be skipped
	mkfile(t, filepath.Join(dir, ".npm", "cache.dat"))

	result := Search(dir, "dat", 10)
	if len(result) != 0 {
		t.Fatalf("expected 0 results (all in skip dirs), got %d: %v", len(result), result)
	}

	result = Search(dir, "json", 10)
	if len(result) != 0 {
		t.Fatalf("expected 0 results (.pnpm-store skipped), got %d: %v", len(result), result)
	}
}

func TestSearchQueryTrim(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "main.go"))

	result := Search(dir, "  main  ", 10)
	if len(result) != 1 {
		t.Fatalf("expected 1 result after trimming query, got %d: %v", len(result), result)
	}
}

func TestSearchWalkError(t *testing.T) {
	// Walk a non-existent directory - should not panic and return nil
	result := Search("/nonexistent_path_xyz_123", "test", 10)
	if len(result) != 0 {
		t.Fatalf("expected 0 results for non-existent dir, got %d: %v", len(result), result)
	}
}

func TestSearchSymlinkDir(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "realdir")
	targetDir := filepath.Join(dir, "linkdir")
	os.MkdirAll(realDir, 0o755)
	os.Symlink(realDir, targetDir)
	mkfile(t, filepath.Join(realDir, "inside.txt"))

	result := Search(dir, "inside", 10)
	// Symlinks to dirs are followed; the file might be discovered or not
	// depending on OS. Just ensure no panic.
	_ = result
}

func TestSearchPartialMatch(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "my-component.tsx"))
	mkfile(t, filepath.Join(dir, "component.ts"))
	mkfile(t, filepath.Join(dir, "unrelated.py"))

	result := Search(dir, "comp", 10)
	if len(result) != 2 {
		t.Fatalf("expected 2 partial matches for 'comp', got %d: %v", len(result), result)
	}
}

func TestSearchMaxWalkEntriesConstant(t *testing.T) {
	if maxWalkEntries != 10000 {
		t.Fatalf("expected maxWalkEntries = 10000, got %d", maxWalkEntries)
	}
}

func TestSearchMinQueryLenConstant(t *testing.T) {
	if minQueryLen != 2 {
		t.Fatalf("expected minQueryLen = 2, got %d", minQueryLen)
	}
}
