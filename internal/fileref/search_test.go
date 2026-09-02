package fileref

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeFile is a tiny helper that creates a regular file with placeholder
// content. Tests use it to scaffold the workspace layout before each Search
// call.
func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// resultPaths extracts just the Path fields from a slice of SearchResult for
// easy assertion against expected path lists.
func resultPaths(results []SearchResult) []string {
	paths := make([]string, len(results))
	for i, r := range results {
		paths[i] = r.Path
	}
	return paths
}

// containsPath reports whether want appears in the results by Path field.
func containsPath(got []SearchResult, want string) bool {
	for _, r := range got {
		if r.Path == want {
			return true
		}
	}
	return false
}

// containsDirHit reports whether a result with the given Path and IsDir=true
// exists in the results.
func containsDirHit(got []SearchResult, wantPath string) bool {
	for _, r := range got {
		if r.Path == wantPath && r.IsDir {
			return true
		}
	}
	return false
}

// TestSearchMatchesPathSegment verifies the fix for issue #3769: a query
// matching an intermediate directory segment (here "planind") should now
// surface files under that directory, even when the basename does not
// contain the query.
func TestSearchMatchesPathSegment(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "planind", "index.tsx"))

	got := Search(root, "planind", 50)
	if !containsPath(got, "src/planind/index.tsx") {
		t.Fatalf("Search(%q) should return %q (path-segment match), got %v", "planind", "src/planind/index.tsx", resultPaths(got))
	}
}

// TestSearchMatchesDirectories verifies that a query matching a directory
// name returns the directory itself with IsDir=true, not just its contents.
func TestSearchMatchesDirectories(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "assets", "readme.md"))
	writeFile(t, filepath.Join(root, "docs", "assets", "image.png"))

	got := Search(root, "assets", 50)
	if !containsDirHit(got, "docs/assets") {
		t.Fatalf("Search(%q) should return directory %q with IsDir=true, got %v", "assets", "docs/assets", resultPaths(got))
	}
	// Directory hits come after basename and segment hits.
	if !containsPath(got, "docs/assets/readme.md") {
		t.Fatalf("Search(%q) should still return files under the directory, got %v", "assets", resultPaths(got))
	}
}

// TestSearchKeepsBasenameMatch guards the legacy behavior: when the query
// matches the file's basename, the file must still appear in the results,
// and basename hits must sort strictly before path-segment and directory
// hits so the most relevant matches surface at the top of the completion
// menu.
func TestSearchKeepsBasenameMatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "planind.go"))
	writeFile(t, filepath.Join(root, "src", "planind", "index.tsx")) // also a segment hit

	got := Search(root, "planind", 50)
	gotPaths := resultPaths(got)
	want := []string{"src/planind", "planind.go", "src/planind/index.tsx"}
	if !equalSlices(gotPaths, want) {
		t.Fatalf("Search(%q) order mismatch:\n  want %v\n  got  %v", "planind", want, gotPaths)
	}
	// Verify the directory hit has IsDir=true.
	if !containsDirHit(got, "src/planind") {
		t.Fatalf("Search(%q) should return %q with IsDir=true", "planind", "src/planind")
	}
}

// equalSlices reports whether two []string are element-wise equal.
func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSearchHandlesBasenamePathQuery verifies that searching for the basename
// part of a nested file still surfaces the file (regression guard).
func TestSearchHandlesBasenamePathQuery(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "planind", "index.tsx"))

	got := Search(root, "index", 50)
	if !containsPath(got, "src/planind/index.tsx") {
		t.Fatalf("Search(%q) should return %q (basename of nested file), got %v", "index", "src/planind/index.tsx", resultPaths(got))
	}
}

// TestSearchSkipsNoiseStillWorks ensures the noise-directory skip list still
// applies to path-segment matches. Files under node_modules must not surface
// even when an intermediate segment matches the query.
func TestSearchSkipsNoiseStillWorks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "planind", "index.tsx"))          // legitimate hit
	writeFile(t, filepath.Join(root, "node_modules", "planind", "index.tsx")) // must be skipped
	writeFile(t, filepath.Join(root, "build", "planind", "index.tsx"))        // skipDirNames
	writeFile(t, filepath.Join(root, "dist", "planind", "index.tsx"))         // skipDirNames

	got := Search(root, "planind", 50)
	if containsPath(got, "node_modules/planind/index.tsx") {
		t.Fatalf("Search should skip node_modules, got %v", resultPaths(got))
	}
	if containsPath(got, "build/planind/index.tsx") {
		t.Fatalf("Search should skip build/, got %v", resultPaths(got))
	}
	if containsPath(got, "dist/planind/index.tsx") {
		t.Fatalf("Search should skip dist/, got %v", resultPaths(got))
	}
	if !containsPath(got, "src/planind/index.tsx") {
		t.Fatalf("Search should still return legitimate hit, got %v", resultPaths(got))
	}
}

func TestSearchSurfacesExactlyTypedHiddenEntry(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "secret.txt"))
	writeFile(t, filepath.Join(root, "src", "secret.go"))
	writeFile(t, filepath.Join(root, "src", "visible.go"))
	os.MkdirAll(filepath.Join(root, "secret-dir"), 0o755)

	filter, err := NewFilter(root, FilterOptions{ExcludePatterns: []string{"secret.txt"}})
	if err != nil {
		t.Fatalf("NewFilter: %v", err)
	}

	got := SearchWithFilter(root, "secret.txt", 50, filter)
	if !containsPath(got, "secret.txt") {
		t.Fatalf("exact-name hidden file should be surfaced, got %v", resultPaths(got))
	}
	for _, r := range got {
		if r.Path == "secret.txt" && !r.Hidden {
			t.Fatalf("secret.txt should be marked Hidden, got %+v", r)
		}
		if r.Path == "src/secret.go" && r.Hidden {
			t.Fatalf("src/secret.go is not filtered and must not be Hidden, got %+v", r)
		}
	}

	filterDir, err := NewFilter(root, FilterOptions{ExcludePatterns: []string{"secret-dir"}})
	if err != nil {
		t.Fatalf("NewFilter: %v", err)
	}
	dirGot := SearchWithFilter(root, "secret-dir", 50, filterDir)
	if !containsDirHit(dirGot, "secret-dir") {
		t.Fatalf("exactly typed excluded dir should be surfaced, got %v", resultPaths(dirGot))
	}
	for _, r := range dirGot {
		if r.Path == "secret-dir" && !r.Hidden {
			t.Fatalf("secret-dir should be marked Hidden, got %+v", r)
		}
	}

	partial := SearchWithFilter(root, "secret.tx", 50, filter)
	if containsPath(partial, "secret.txt") {
		t.Fatalf("partial query must not surface hidden entry, got %v", resultPaths(partial))
	}
}

// TestSearchHiddenExactSurvivesTruncation verifies that an exactly typed
// hidden entry is not crowded out by the dirQuota or limit truncation: hidden
// hits sort ahead of ordinary ones in their tier, so the picker can still
// surface a manually typed @path even under heavy competition.
func TestSearchHiddenExactSurvivesTruncation(t *testing.T) {
	root := t.TempDir()
	// Hidden exact target plus many lexicographically-earlier visible files.
	writeFile(t, filepath.Join(root, "secret.txt"))
	for i := 0; i < 30; i++ {
		writeFile(t, filepath.Join(root, "a-secret-file-000", fmt.Sprintf("f%02d", i)))
		os.MkdirAll(filepath.Join(root, "a-secret-dir-000", "sub"), 0o755)
	}
	os.MkdirAll(filepath.Join(root, "secret-dir"), 0o755)

	filter, err := NewFilter(root, FilterOptions{ExcludePatterns: []string{"secret.txt", "secret-dir"}})
	if err != nil {
		t.Fatalf("NewFilter: %v", err)
	}

	// Small limit forces basename truncation; hidden file must stay first.
	got := SearchWithFilter(root, "secret.txt", 5, filter)
	if len(got) == 0 || got[0].Path != "secret.txt" || !got[0].Hidden {
		t.Fatalf("hidden exact file should lead results, got %v", resultPaths(got))
	}

	// Directory quota: hidden dir must survive even with 5+ other dir hits.
	dirGot := SearchWithFilter(root, "secret-dir", 50, filter)
	if len(dirGot) == 0 || dirGot[0].Path != "secret-dir" || !dirGot[0].Hidden {
		t.Fatalf("hidden exact dir should lead dir results, got %v", resultPaths(dirGot))
	}
}
