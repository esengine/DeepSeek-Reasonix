//go:build !windows

package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

func realDir(t *testing.T, path string) string {
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

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// outsideHostDirs plans as if the test's temp tree were no host write
// directory, which it is on macOS.
func outsideHostDirs(t *testing.T) {
	t.Helper()
	prev := hostWritePins
	hostWritePins = func() *WriteRootPins { return nil }
	t.Cleanup(func() { hostWritePins = prev })
}

func dropCodes(plan writeRootPlan) map[string]string {
	out := map[string]string{}
	for _, d := range plan.dropped {
		out[d.Root] = d.Code
	}
	return out
}

func TestPlanKeepsResolvedRootsNamedAsGiven(t *testing.T) {
	base := testenv.TempDir(t)
	ws := realDir(t, filepath.Join(base, "ws"))
	sub := realDir(t, filepath.Join(ws, "sub"))
	spec := Spec{WriteRoots: []string{filepath.Join(base, "ws"), filepath.Join(base, "ws", "sub")}, MinimalWrites: true}
	spec.Pins = PinWriteRoots(spec.WriteRoots)
	plan := planWriteRoots(spec, nil)
	if len(plan.dropped) != 0 || !slices.Equal(plan.dirs, []string{ws, sub}) {
		t.Fatalf("plan = %+v, want both roots resolved and none dropped", plan)
	}
}

func TestPlanRefusesARootReachedThroughALinkInAWritableDir(t *testing.T) {
	base := testenv.TempDir(t)
	ws := realDir(t, filepath.Join(base, "ws"))
	outside := realDir(t, filepath.Join(base, "outside"))
	realDir(t, filepath.Join(outside, "b"))
	later, nested := filepath.Join(ws, "later"), filepath.Join(ws, "a", "b")
	for _, pinned := range []bool{false, true} {
		spec := Spec{WriteRoots: []string{ws, later, nested}, MinimalWrites: true}
		if pinned {
			realDir(t, later)
			spec.Pins = PinWriteRoots(spec.WriteRoots)
			if err := os.Remove(later); err != nil {
				t.Fatal(err)
			}
		}
		symlink(t, outside, later)
		symlink(t, outside, filepath.Join(ws, "a"))
		plan := planWriteRoots(spec, nil)
		codes := dropCodes(plan)
		if codes[later] != WriteRootRedirectedCode || codes[nested] != WriteRootRedirectedCode {
			t.Fatalf("pinned=%v drops = %v, want both linked roots redirected", pinned, codes)
		}
		if slices.ContainsFunc(plan.dirs, func(d string) bool { return pathWithin(outside, d) }) || !slices.Equal(plan.dirs, []string{ws}) {
			t.Fatalf("pinned=%v dirs = %v, want only the workspace", pinned, plan.dirs)
		}
		_ = os.Remove(later)
		_ = os.Remove(filepath.Join(ws, "a"))
	}
}

func TestPlanFollowsALinkNoConfinedCommandCanRewrite(t *testing.T) {
	outsideHostDirs(t)
	base := testenv.TempDir(t)
	target := realDir(t, filepath.Join(base, "real"))
	link := filepath.Join(base, "link")
	symlink(t, target, link)
	spec := Spec{WriteRoots: []string{link}, MinimalWrites: true}
	spec.Pins = PinWriteRoots(spec.WriteRoots)
	plan := planWriteRoots(spec, nil)
	if len(plan.dropped) != 0 || !slices.Equal(plan.dirs, []string{target}) {
		t.Fatalf("plan = %+v, want the link's target", plan)
	}
}

func TestPlanRefusesAPinnedRootThatIsNoLongerTheSameDirectory(t *testing.T) {
	base := testenv.TempDir(t)
	ws := realDir(t, filepath.Join(base, "ws"))
	out := realDir(t, filepath.Join(base, "out"))
	spec := Spec{WriteRoots: []string{ws, out}, MinimalWrites: true}
	spec.Pins = PinWriteRoots(spec.WriteRoots)
	// The old directory stays alive under another name, so the new one
	// cannot reuse its inode.
	if err := os.Rename(out, out+".old"); err != nil {
		t.Fatal(err)
	}
	if codes := dropCodes(planWriteRoots(spec, nil)); codes[out] != WriteRootChangedCode {
		t.Fatalf("removed root drops = %v, want changed", codes)
	}
	realDir(t, out)
	plan := planWriteRoots(spec, nil)
	if codes := dropCodes(plan); codes[out] != WriteRootChangedCode || codes[ws] != "" {
		t.Fatalf("replaced root drops = %v, want only it changed", codes)
	}
}

func TestPlanAcceptsAPinnedRootCreatedInPlace(t *testing.T) {
	base := testenv.TempDir(t)
	ws := realDir(t, filepath.Join(base, "ws"))
	later := filepath.Join(ws, "later")
	spec := Spec{WriteRoots: []string{ws, later}, MinimalWrites: true}
	spec.Pins = PinWriteRoots(spec.WriteRoots)
	realDir(t, later)
	plan := planWriteRoots(spec, nil)
	if len(plan.dropped) != 0 || !slices.Equal(plan.dirs, []string{ws, later}) {
		t.Fatalf("plan = %+v, want the root created where it was pinned", plan)
	}
}

func TestPlanARootRepointedAtTheRootDirectoryRefusesNoOther(t *testing.T) {
	outsideHostDirs(t)
	base := testenv.TempDir(t)
	ws := realDir(t, filepath.Join(base, "ws"))
	target := realDir(t, filepath.Join(base, "real"))
	link := filepath.Join(base, "link")
	symlink(t, target, link)
	later := filepath.Join(ws, "later")
	symlink(t, "/", later)
	plan := planWriteRoots(Spec{WriteRoots: []string{ws, later, link}, MinimalWrites: true}, nil)
	if codes := dropCodes(plan); len(codes) != 1 || codes[later] != WriteRootRedirectedCode {
		t.Fatalf("drops = %v, want only the re-pointed root", codes)
	}
	if !slices.Equal(plan.dirs, []string{ws, target}) {
		t.Fatalf("dirs = %v", plan.dirs)
	}
}

func TestPlanRefusesAClaimLinkedInsideTheSessionsPinnedRoots(t *testing.T) {
	base := testenv.TempDir(t)
	ws := realDir(t, filepath.Join(base, "ws"))
	outside := realDir(t, filepath.Join(base, "outside"))
	pins := PinWriteRoots([]string{ws})
	claim := filepath.Join(ws, "claim")
	symlink(t, outside, claim)
	plan := planWriteRoots(Spec{WriteRoots: []string{claim}, Pins: pins, MinimalWrites: true}, nil)
	if codes := dropCodes(plan); codes[claim] != WriteRootRedirectedCode || len(plan.dirs) != 0 {
		t.Fatalf("plan = %+v, want the claim refused", plan)
	}
}

func TestDroppedWriteRootsOnlyWhenEnforcing(t *testing.T) {
	base := testenv.TempDir(t)
	ws := realDir(t, filepath.Join(base, "ws"))
	later := filepath.Join(ws, "later")
	symlink(t, base, later)
	spec := Spec{WriteRoots: []string{ws, later}, MinimalWrites: true}
	if got := DroppedWriteRoots(spec); got != nil {
		t.Fatalf("unenforced drops = %v", got)
	}
	spec.Mode = "enforce"
	if got := DroppedWriteRoots(spec); len(got) != 1 || got[0].Root != later || got[0].Code != WriteRootRedirectedCode {
		t.Fatalf("drops = %v", got)
	}
}

// A host cache's own entry is inside the grant it names, so a confined command
// can move it aside and leave a link there; the pinned identity refuses it.
func TestPlanRefusesAHostDirectoryWhoseOwnEntryBecameALink(t *testing.T) {
	base := testenv.TempDir(t)
	ws := realDir(t, filepath.Join(base, "ws"))
	cache := realDir(t, filepath.Join(base, "home", ".cache"))
	gopath := realDir(t, filepath.Join(base, "home", "go"))
	agents := realDir(t, filepath.Join(base, "home", "Library", "LaunchAgents"))
	spec := Spec{WriteRoots: []string{ws}, MinimalWrites: true}
	spec.Pins = PinWriteRoots([]string{ws, cache, gopath})
	if err := os.Rename(gopath, filepath.Join(cache, "stash")); err != nil {
		t.Fatal(err)
	}
	symlink(t, agents, gopath)
	plan := planWriteRoots(spec, []string{cache, gopath})
	if codes := dropCodes(plan); codes[gopath] != WriteRootRedirectedCode {
		t.Fatalf("drops = %v, want the re-pointed cache refused", codes)
	}
	if slices.Contains(plan.dirs, agents) {
		t.Fatalf("dirs = %v grant the link's target", plan.dirs)
	}
}

func TestWithKeepsTheIdentityAlreadyPinned(t *testing.T) {
	base := testenv.TempDir(t)
	ws := realDir(t, filepath.Join(base, "ws"))
	outside := realDir(t, filepath.Join(base, "outside"))
	pins := PinWriteRoots([]string{ws})
	if err := os.Rename(ws, ws+".old"); err != nil {
		t.Fatal(err)
	}
	symlink(t, outside, ws)
	id, _ := pins.With([]string{ws}).lookup(ws)
	if id.path != ws {
		t.Fatalf("re-pinning moved the identity to %s", id.path)
	}
}

func TestHostWriteDirectoriesPinOnlyTheirPath(t *testing.T) {
	for key, id := range pinHostWriteDirs().roots {
		if id.info != nil {
			t.Fatalf("%s pins a file identity; a cache rebuilt in place would be refused", key)
		}
	}
}
