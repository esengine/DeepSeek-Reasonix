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

// TestSearchSurfacesHiddenEntries guards the removal of the showHidden gate:
// dot-prefixed directories and files must appear in search results even when
// the query itself does not start with a dot.
func TestSearchSurfacesHiddenEntries(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".vscode", "settings.json"))

	// A plain query (no leading dot) must surface hidden files/dirs that are
	// NOT worktree copies (those are outside the file-list scope).
	got := Search(root, "settings", 50)
	if !containsPath(got, ".vscode/settings.json") {
		t.Fatalf("Search(%q) should surface hidden file, got %v", "settings", resultPaths(got))
	}
}

// TestSearchSurfacesReadmesUnderHiddenDirs verifies that README files living
// under dot-prefixed worktree dirs (.worktrees, .cindy-worktrees) surface for
// a plain "README" query — worktree copies are in scope, not excluded.
func TestSearchSurfacesReadmesUnderHiddenDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"))
	writeFile(t, filepath.Join(root, ".worktrees", "feature-x", "README.md"))
	writeFile(t, filepath.Join(root, ".cindy-worktrees", "branch-a", "README.md"))

	got := Search(root, "README", 50)
	for _, want := range []string{
		"README.md",
		".worktrees/feature-x/README.md",
		".cindy-worktrees/branch-a/README.md",
	} {
		if !containsPath(got, want) {
			t.Fatalf("Search(%q) should surface %q, got %v", "README", want, resultPaths(got))
		}
	}
}

// TestSearchLimitTruncatesReadmes reproduces the real repo: many worktree
// copies under dot-prefixed dirs each carry a README, and a small result
// limit truncates the alphabetically later ones (e.g. .worktrees/...).
func TestSearchLimitTruncatesReadmes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"))
	writeFile(t, filepath.Join(root, ".worktrees", "chat-history-sidebar", "README.md"))
	for i := 0; i < 25; i++ {
		writeFile(t, filepath.Join(root, ".cindy-worktrees", fmt.Sprintf("branch-%02d", i), "README.md"))
	}
	got := Search(root, "README", 100)
	if !containsPath(got, ".worktrees/chat-history-sidebar/README.md") {
		t.Fatalf("Search(limit=100) truncated .worktrees README: %v", resultPaths(got))
	}
}
