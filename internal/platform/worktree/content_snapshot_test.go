package worktree

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnapshotHashesBytesDespiteTrustedCachedStat(t *testing.T) {
	root := t.TempDir()
	gitIn(t, root, "init", "-q")
	gitIn(t, root, "config", "core.trustctime", "false")
	gitIn(t, root, "config", "core.checkstat", "minimal")
	writeRepoFile(t, root, "input", "old\n")
	writeRepoFile(t, root, ".gitignore", "staged-ignored\n")
	path := filepath.Join(root, "input")
	stamp := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-qm", "base")
	writeRepoFile(t, root, "staged-ignored", "must survive\n")
	gitIn(t, root, "add", "-f", "staged-ignored")
	index := filepath.Join(root, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, root, "input", "new\n")
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	// The ordinary stat cache really does hide this edit; otherwise the test
	// would not distinguish hashing bytes from copying Git's cached version.
	if diff := gitIn(t, root, "diff", "--", "input"); diff != "" {
		t.Fatalf("fixture did not reproduce a trusted stale stat cache: %s", diff)
	}
	snapshot, err := TakeSnapshot(t.Context(), opened(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if got := gitIn(t, root, "show", snapshot.Tree+":input"); got != "new\n" {
		t.Fatalf("snapshot reused stale stat cache: %q", got)
	}
	if got := gitIn(t, root, "show", snapshot.Tree+":staged-ignored"); got != "must survive\n" {
		t.Fatalf("private index rebuild lost staged ignored input: %q", got)
	}
	after, err := os.ReadFile(index)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("snapshot modified the user's index: %v", err)
	}
	if got := strings.TrimSpace(gitIn(t, root, "show", "HEAD:input")); got != "old" {
		t.Fatalf("snapshot changed HEAD: %s", got)
	}
}
