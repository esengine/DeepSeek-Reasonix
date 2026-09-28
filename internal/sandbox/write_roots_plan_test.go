//go:build !windows

package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// outsideHostDirs plans as if the test's temp tree were no host write
// directory, which it is on macOS.
func outsideHostDirs(t *testing.T) {
	t.Helper()
	prev := hostWritePins
	hostWritePins = func() hostPins { return nil }
	t.Cleanup(func() { hostWritePins = prev })
}

func planDir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func planLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestPlanRefusesARootReachedThroughALinkInAWritableDir(t *testing.T) {
	base := t.TempDir()
	ws := planDir(t, filepath.Join(base, "ws"))
	outside := planDir(t, filepath.Join(base, "outside"))
	planDir(t, filepath.Join(outside, "b"))
	later, nested := filepath.Join(ws, "later"), filepath.Join(ws, "a", "b")
	planLink(t, outside, later)
	planLink(t, outside, filepath.Join(ws, "a"))
	plan := planWriteRoots([]string{ws, later, nested}, nil, "")
	if !slices.Equal(plan.dirs, []string{ws}) || len(plan.refused) != 2 {
		t.Fatalf("plan = %+v, want only the workspace", plan)
	}
}

func TestPlanFollowsALinkNoConfinedCommandCanRewrite(t *testing.T) {
	outsideHostDirs(t)
	base := t.TempDir()
	target := planDir(t, filepath.Join(base, "real"))
	link := filepath.Join(base, "link")
	planLink(t, target, link)
	plan := planWriteRoots([]string{link}, nil, "")
	if len(plan.refused) != 0 || !slices.Equal(plan.dirs, []string{target}) {
		t.Fatalf("plan = %+v, want the link's target", plan)
	}
}

func TestPlanARootRepointedAtTheRootDirectoryRefusesNoOther(t *testing.T) {
	outsideHostDirs(t)
	base := t.TempDir()
	ws := planDir(t, filepath.Join(base, "ws"))
	target := planDir(t, filepath.Join(base, "real"))
	link := filepath.Join(base, "link")
	planLink(t, target, link)
	later := filepath.Join(ws, "later")
	planLink(t, "/", later)
	plan := planWriteRoots([]string{ws, later, link}, nil, "")
	if !slices.Equal(plan.dirs, []string{ws, target}) || len(plan.refused) != 1 {
		t.Fatalf("plan = %+v, want only the re-pointed root refused", plan)
	}
}

// A host cache's own entry is inside the grant it names, so a confined command
// can move it aside and leave a link there; the pinned identity refuses it.
func TestPlanRefusesAHostDirectoryWhoseOwnEntryBecameALink(t *testing.T) {
	base := t.TempDir()
	ws := planDir(t, filepath.Join(base, "ws"))
	cache := planDir(t, filepath.Join(base, "home", ".cache"))
	gopath := planDir(t, filepath.Join(base, "home", "go"))
	agents := planDir(t, filepath.Join(base, "home", "Library", "LaunchAgents"))
	prev := hostWritePins
	pins := hostPins{}
	for _, d := range []string{cache, gopath} {
		info, _ := os.Stat(d)
		pins[d] = hostIdentity{path: d, info: info}
	}
	hostWritePins = func() hostPins { return pins }
	t.Cleanup(func() { hostWritePins = prev })
	if err := os.Rename(gopath, filepath.Join(cache, "stash")); err != nil {
		t.Fatal(err)
	}
	planLink(t, agents, gopath)
	plan := planWriteRoots([]string{ws}, []string{cache, gopath}, "")
	if slices.Contains(plan.dirs, agents) || len(plan.refused) != 1 {
		t.Fatalf("plan = %+v, want the re-pointed cache refused", plan)
	}
}

func TestHostWriteDirectoriesPinOnlyTheirPath(t *testing.T) {
	for key, id := range pinHostWriteDirs() {
		if id.info != nil {
			t.Fatalf("%s pins a file identity; a cache rebuilt in place would be refused", key)
		}
	}
}
