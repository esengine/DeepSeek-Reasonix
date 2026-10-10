package sandbox

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestReadGitInstallRootsAsksEveryRootAndViewAndKeepsTheAnswers(t *testing.T) {
	type ask struct {
		root registry.Key
		view uint32
	}
	var asked []ask
	got := readGitInstallRoots(func(root registry.Key, view uint32) string {
		asked = append(asked, ask{root, view})
		if root == registry.CURRENT_USER && view == registry.WOW64_64KEY {
			return `D:\Portable\Git`
		}
		if root == registry.LOCAL_MACHINE && view == registry.WOW64_32KEY {
			return `E:\Git`
		}
		return ""
	})
	if len(asked) != 4 {
		t.Fatalf("asked %d registry views, want machine and user in both views", len(asked))
	}
	if strings.Join(got, "|") != `E:\Git|D:\Portable\Git` {
		t.Fatalf("got %q, want the machine-wide answer first, then the per-user one", got)
	}
}

// A machine without Git for Windows has no such key; reading it must come back
// empty rather than fail, since discovery runs at every start.
func TestGitInstallRootsReadsTheRealRegistryWithoutFailing(t *testing.T) {
	for _, r := range gitInstallRoots() {
		if r == "" {
			t.Fatal("an empty root was returned")
		}
	}
}
