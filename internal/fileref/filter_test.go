package fileref

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFilterFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSearchWithFilterCustomRulesApplyToBrowseAndSearch(t *testing.T) {
	root := t.TempDir()
	writeFilterFile(t, filepath.Join(root, "keep", "needle.txt"), "ok")
	writeFilterFile(t, filepath.Join(root, "local_npm", "needle.txt"), "hidden")
	writeFilterFile(t, filepath.Join(root, "config.local.json"), "hidden")

	filter, err := NewFilter(root, FilterOptions{ExcludePatterns: []string{"local_npm/**", "config.local.json"}})
	if err != nil {
		t.Fatal(err)
	}
	if !filter.Skip("local_npm", "local_npm", true) {
		t.Fatal("custom folder rule must prune the folder during browsing")
	}
	got := SearchWithFilter(root, "needle", 20, filter)
	if !containsPath(got, "keep/needle.txt") {
		t.Fatalf("allowed file missing from filtered search: %v", resultPaths(got))
	}
	if containsPath(got, "local_npm/needle.txt") {
		t.Fatalf("custom folder rule leaked into search: %v", resultPaths(got))
	}
	if filter.Skip("config.local.json", "config.local.json", false) != true {
		t.Fatal("custom file rule must hide the file")
	}
}

func TestFollowGitignoreIsOptInAndSupportsNestedNegationAndGlobalSources(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFilterFile(t, filepath.Join(root, ".gitignore"), "ignored.txt\nbuild/\n")
	writeFilterFile(t, filepath.Join(root, "src", ".gitignore"), "generated.go\n!keep.go\n")
	writeFilterFile(t, filepath.Join(root, ".git", "info", "exclude"), "info-only.txt\n")
	writeFilterFile(t, filepath.Join(root, "ignored.txt"), "hidden")
	writeFilterFile(t, filepath.Join(root, "build", "ignored-build.txt"), "hidden")
	writeFilterFile(t, filepath.Join(root, "src", "generated.go"), "hidden")
	writeFilterFile(t, filepath.Join(root, "src", "keep.go"), "kept")
	writeFilterFile(t, filepath.Join(root, "info-only.txt"), "hidden")

	globalDir := t.TempDir()
	globalIgnore := filepath.Join(globalDir, "global-ignore")
	globalConfig := filepath.Join(globalDir, "gitconfig")
	writeFilterFile(t, globalIgnore, "global-only.txt\n")
	writeFilterFile(t, globalConfig, "[core]\n\texcludesFile = "+globalIgnore+"\n")
	writeFilterFile(t, filepath.Join(root, "global-only.txt"), "hidden")
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)

	off, err := NewFilter(root, FilterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if off.Skip("ignored.txt", "ignored.txt", false) {
		t.Fatal("Git ignore rules must not apply while follow_gitignore is off")
	}

	on, err := NewFilter(root, FilterOptions{FollowGitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"ignored.txt", "build", "src/generated.go", "info-only.txt", "global-only.txt"} {
		if !on.Ignored(filepath.Join(root, filepath.FromSlash(path)), filepath.Ext(path) == "") {
			t.Fatalf("Git ignore rule did not hide %s", path)
		}
	}
	if on.Ignored(filepath.Join(root, "src", "keep.go"), false) {
		t.Fatal("nested negation must keep src/keep.go visible")
	}
}

func (f Filter) Ignored(path string, isDir bool) bool {
	if f.gitignore == nil {
		return false
	}
	return f.gitignore.Ignored(path, isDir)
}
