package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestWritableRootSetSnapshotAndMissing(t *testing.T) {
	base := t.TempDir()
	extra := filepath.Join(t.TempDir(), "extra")
	set := NewWritableRootSet([]string{base})
	if !set.Covers(filepath.Join(base, "src")) {
		t.Fatal("workspace child should be covered")
	}
	if set.Covers(extra) {
		t.Fatal("unrelated dir should not be covered")
	}
	missing := set.Missing([]string{extra, filepath.Join(base, "pkg")})
	if len(missing) != 1 || !PathWithin(canonicalDir(extra), missing[0]) {
		t.Fatalf("Missing = %v, want [%s]", missing, extra)
	}
	set.GrantSession([]string{extra})
	if !set.Covers(filepath.Join(extra, "bin")) {
		t.Fatal("session grant should cover children")
	}
	if len(set.Missing([]string{extra})) != 0 {
		t.Fatal("granted dir should not be missing")
	}
	set.ClearSession()
	if set.Covers(extra) {
		t.Fatal("ClearSession should drop session grants")
	}
}

func TestWritableRootSetReplaceBaselineKeepsSession(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	sess := t.TempDir()
	set := NewWritableRootSet([]string{a})
	set.GrantSession([]string{sess})
	set.ReplaceBaseline([]string{b})
	if set.Covers(a) {
		t.Fatal("old baseline should be gone")
	}
	if !set.Covers(b) || !set.Covers(sess) {
		t.Fatal("new baseline and session grant should remain")
	}
}

func TestWritableRootSetVerifiedBaselineSurvivesSessionClear(t *testing.T) {
	base := t.TempDir()
	project := t.TempDir()
	set := NewWritableRootSet([]string{base})
	set.GrantVerifiedBaseline([]string{canonicalDir(project)})
	set.ClearSession()
	if !set.Covers(project) {
		t.Fatal("project grant should remain in the baseline after session clear")
	}
}

func TestWritableRootSetPerCallDoesNotLeak(t *testing.T) {
	base := t.TempDir()
	once := canonicalDir(t.TempDir())
	set := NewWritableRootSet([]string{base})
	ctx := WithPerCallWriteRoots(context.Background(), []string{once})
	if got := set.Effective(ctx); !containsRoot(got, once) {
		t.Fatalf("Effective should include per-call root, got %v", got)
	}
	if set.Covers(once) {
		t.Fatal("per-call root must not enter the session snapshot")
	}
}

func TestWritableRootSetConcurrentReads(t *testing.T) {
	set := NewWritableRootSet([]string{t.TempDir()})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_ = set.Snapshot()
			set.GrantSession([]string{t.TempDir()})
			_ = set.Effective(context.Background())
		})
	}
	wg.Wait()
}

func TestIntersectWriteRoots(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "src")
	other := t.TempDir()
	got := IntersectWriteRoots([]string{root}, []string{child, other})
	if len(got) != 1 || !PathWithin(canonicalDir(child), got[0]) {
		t.Fatalf("IntersectWriteRoots = %v, want [%s]", got, child)
	}
	if len(IntersectWriteRoots([]string{root}, []string{other})) != 0 {
		t.Fatal("disjoint roots should intersect to empty")
	}
}

func TestCloneRestricted(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "pkg")
	sess := t.TempDir()
	set := NewWritableRootSet([]string{root})
	set.GrantSession([]string{sess})
	restricted := set.CloneRestricted([]string{child})
	if !restricted.Covers(child) {
		t.Fatal("restricted view should keep the intersection")
	}
	if restricted.Covers(sess) {
		t.Fatal("write_paths intersection must drop unrelated session grants")
	}
	inherited := set.CloneRestricted(nil)
	if !inherited.Covers(sess) || !inherited.Covers(root) {
		t.Fatal("empty cap should copy the current snapshot")
	}
}

func TestStableWriteRootsDropsRetargetedIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not guaranteed on Windows runners")
	}
	root := canonicalDir(t.TempDir())
	approved := filepath.Join(root, "approved")
	redirected := filepath.Join(root, "redirected")
	if err := os.MkdirAll(approved, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(redirected, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(approved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(redirected, approved); err != nil {
		t.Fatal(err)
	}
	set := newVerifiedWritableRootSet([]string{approved})
	if got := set.EffectiveSandboxRoots(context.Background()); len(got) != 0 {
		t.Fatalf("retargeted root must fail closed, got %v", got)
	}
	if set.Covers(approved) {
		t.Fatal("retargeted root must no longer count as covered")
	}
	if got := set.Missing([]string{approved}); len(got) != 1 {
		t.Fatalf("retargeted root must be eligible for re-approval, got %v", got)
	}
}

func containsRoot(roots []string, want string) bool {
	want = canonicalDir(want)
	for _, root := range roots {
		if PathWithin(root, want) {
			return true
		}
	}
	return false
}
