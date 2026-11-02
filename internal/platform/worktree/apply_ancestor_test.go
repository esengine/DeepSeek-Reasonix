package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestWriteChangesRejectsRedirectedDeletion(t *testing.T) {
	repo := initRepo(t)
	outside := testenv.TempDir(t)
	writeRepoFile(t, outside, "victim.txt", "original\n")
	linkApplyDirectory(t, outside, filepath.Join(repo, "nested"))
	err := writeChanges(t.Context(), opened(t, repo), "HEAD", []Change{{Status: "D", Path: "nested/victim.txt"}})
	if err == nil {
		t.Fatal("publication accepted a redirected deletion")
	}
	if got := readRepoFile(t, outside, "victim.txt"); got != "original\n" {
		t.Fatalf("outside file changed: %q", got)
	}
}

func TestWriteChangesUnlinksLeafDirectoryLink(t *testing.T) {
	repo := initRepo(t)
	outside := testenv.TempDir(t)
	writeRepoFile(t, outside, "victim.txt", "original\n")
	link := filepath.Join(repo, "link")
	linkApplyDirectory(t, outside, link)
	if err := writeChanges(t.Context(), opened(t, repo), "HEAD", []Change{{Status: "D", Path: "link"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("leaf link was not removed: %v", err)
	}
	if got := readRepoFile(t, outside, "victim.txt"); got != "original\n" {
		t.Fatalf("leaf deletion followed the link: %q", got)
	}
}

func linkApplyDirectory(t *testing.T, target, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "mklink", "/J", path, target).CombinedOutput()
		if err != nil {
			t.Fatalf("create directory junction: %v %s", err, out)
		}
	} else if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}
