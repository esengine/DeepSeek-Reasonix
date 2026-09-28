package sandbox

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestBwrapBindsOnlyOutermostExistingResolvedRoots(t *testing.T) {
	base := t.TempDir()
	ws := planDir(t, filepath.Join(base, "ws"))
	outside := planDir(t, filepath.Join(base, "outside"))
	planDir(t, filepath.Join(ws, "sub"))
	link := filepath.Join(base, "wslink")
	planLink(t, ws, link)
	later := filepath.Join(ws, "later")
	planLink(t, outside, later)
	spec := Spec{Mode: "enforce", MinimalWrites: true,
		WriteRoots: []string{link, filepath.Join(ws, "sub"), filepath.Join(ws, "missing"), later}}
	if got := bwrapWriteBinds(linuxWritePlan(spec)); !slices.Equal(got, []string{ws}) {
		t.Fatalf("binds = %v, want only the resolved workspace", got)
	}
	args := bwrapBaseArgs(spec)
	for i := range args {
		if args[i] == "--bind" && (args[i+1] == link || args[i+1] == later || args[i+1] == outside) {
			t.Fatalf("bwrap binds an unresolved or redirected root: %v", args)
		}
	}
}
