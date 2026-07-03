package vcs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectVCS_Git(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectVCS(dir); got != "git" {
		t.Fatalf("DetectVCS = %q, want git", got)
	}
}

func TestDetectVCS_JJ(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectVCS(dir); got != "jj" {
		t.Fatalf("DetectVCS = %q, want jj", got)
	}
}

func TestDetectVCS_JJPreferredOverGit(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectVCS(dir); got != "jj" {
		t.Fatalf("DetectVCS = %q, want jj (preferred over git)", got)
	}
}

func TestDetectVCS_None(t *testing.T) {
	dir := t.TempDir()
	if got := DetectVCS(dir); got != "" {
		t.Fatalf("DetectVCS = %q, want empty", got)
	}
}

func TestDetectVCS_WalkUp(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectVCS(child); got != "jj" {
		t.Fatalf("DetectVCS = %q, want jj (found in ancestor)", got)
	}
}

func TestDetectVCS_EmptyUsesWorkingDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(child)
	if got := DetectVCS(""); got != "git" {
		t.Fatalf("DetectVCS empty cwd = %q, want git from working dir ancestor", got)
	}
}

func TestParseJJDiffStat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		added   int
		removed int
	}{
		{
			name:    "basic",
			input:   " file.go | 5 +++--\n 1 file changed, 3 insertions(+), 2 deletions(-)\n",
			added:   3,
			removed: 2,
		},
		{
			name:    "multiple files",
			input:   " a.go | 2 ++\n b.go | 1 -\n 2 files changed, 2 insertions(+), 1 deletion(-)\n",
			added:   2,
			removed: 1,
		},
		{
			name:    "no changes",
			input:   "",
			added:   0,
			removed: 0,
		},
		{
			name:    "insertions only",
			input:   " new.go | 10 ++++++++++\n 1 file changed, 10 insertions(+)\n",
			added:   10,
			removed: 0,
		},
		{
			name:    "deletions only",
			input:   " old.go | 5 -----\n 1 file changed, 5 deletions(-)\n",
			added:   0,
			removed: 5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			added, removed := parseJJDiffStat(tt.input)
			if added != tt.added || removed != tt.removed {
				t.Fatalf("parseJJDiffStat = (+%d -%d), want (+%d -%d)", added, removed, tt.added, tt.removed)
			}
		})
	}
}

func TestParseJJSummary(t *testing.T) {
	input := "M modified.go\nA new.go\nD deleted.go\nR {old.go => renamed.go}\n? untracked.go\n"
	entries := parseJJSummary(input)
	if len(entries) != 5 {
		t.Fatalf("parseJJSummary returned %d entries, want 5", len(entries))
	}
	tests := []struct {
		path    string
		status  string
		oldPath string
	}{
		{"modified.go", "Modified", ""},
		{"new.go", "Added", ""},
		{"deleted.go", "Deleted", ""},
		{"renamed.go", "Renamed", "old.go"},
		{"untracked.go", "Untracked", ""},
	}
	for i, tt := range tests {
		if entries[i].Path != tt.path || entries[i].Status != tt.status || entries[i].OldPath != tt.oldPath {
			t.Errorf("entry[%d] = %+v, want path=%s status=%s oldPath=%s", i, entries[i], tt.path, tt.status, tt.oldPath)
		}
	}
}

func TestParseJJLog(t *testing.T) {
	input := "abc123\tAlice\t2024-01-15\tFix bug\n" +
		"def456\tBob\t2024-01-16\tAdd feature\n"
	commits := parseJJLog(input)
	if len(commits) != 2 {
		t.Fatalf("parseJJLog returned %d commits, want 2", len(commits))
	}
	if commits[0].Hash != "abc123" || commits[0].Author != "Alice" || commits[0].Message != "Fix bug" {
		t.Errorf("commit[0] = %+v", commits[0])
	}
	if commits[1].Hash != "def456" || commits[1].Author != "Bob" || commits[1].Message != "Add feature" {
		t.Errorf("commit[1] = %+v", commits[1])
	}
}

func TestParseJJRename(t *testing.T) {
	tests := []struct {
		input   string
		oldPath string
		newPath string
	}{
		{"{old => new}", "old", "new"},
		{"{a/b => c/d}", "a/b", "c/d"},
		{"old => new", "old", "new"},
		{"unchanged", "unchanged", "unchanged"},
	}
	for _, tt := range tests {
		oldPath, newPath := parseJJRename(tt.input)
		if oldPath != tt.oldPath || newPath != tt.newPath {
			t.Errorf("parseJJRename(%q) = (%q, %q), want (%q, %q)", tt.input, oldPath, newPath, tt.oldPath, tt.newPath)
		}
	}
}
