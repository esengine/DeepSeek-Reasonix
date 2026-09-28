//go:build linux

package sandbox

import (
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestBwrapBindsOnlyOutermostExistingResolvedRoots(t *testing.T) {
	base := testenv.TempDir(t)
	ws := realDir(t, filepath.Join(base, "ws"))
	outside := realDir(t, filepath.Join(base, "outside"))
	realDir(t, filepath.Join(ws, "sub"))
	link := filepath.Join(base, "wslink")
	symlink(t, ws, link)
	later := filepath.Join(ws, "later")
	symlink(t, outside, later)
	spec := Spec{Mode: "enforce", MinimalWrites: true,
		WriteRoots: []string{link, filepath.Join(ws, "sub"), filepath.Join(ws, "missing"), later}}
	if got := bwrapWriteBinds(spec); !slices.Equal(got, []string{ws}) {
		t.Fatalf("binds = %v, want only the resolved workspace", got)
	}
	args := bwrapBaseArgs(spec)
	for i := range args {
		if args[i] == "--bind" && (args[i+1] == link || args[i+1] == later || args[i+1] == outside) {
			t.Fatalf("bwrap binds an unresolved or redirected root: %v", args)
		}
	}
}

func TestCollapseNestedKeepsOutermostInOrder(t *testing.T) {
	got := collapseNested([]string{"/a/b", "/c", "/a", "/a/b/c", "/c", "/ab"})
	if want := []string{"/c", "/a", "/c", "/ab"}; !slices.Equal(got, want) {
		t.Fatalf("collapse = %v, want %v", got, want)
	}
}
