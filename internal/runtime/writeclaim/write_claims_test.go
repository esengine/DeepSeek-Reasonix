package writeclaim

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
)

func TestNormalizeConcurrencyLimitsZeroIsNoLimit(t *testing.T) {
	total, writers := NormalizeConcurrencyLimits(0, 0)
	if total != 0 || writers != 0 {
		t.Fatalf("got %d/%d, want 0/0 (no limit)", total, writers)
	}
	total, writers = NormalizeConcurrencyLimits(10, 10)
	if total != 10 || writers != 10 {
		t.Fatalf("got %d/%d, want 10/10", total, writers)
	}
	total, writers = NormalizeConcurrencyLimits(4, 10)
	if total != 4 || writers != 4 {
		t.Fatalf("writers must not exceed a set total: got %d/%d", total, writers)
	}
	total, writers = NormalizeConcurrencyLimits(0, 7)
	if total != 0 || writers != 7 {
		t.Fatalf("a writer limit with no total limit must stand: got %d/%d", total, writers)
	}
	total, writers = NormalizeConcurrencyLimits(100, 50)
	if total != 100 || writers != 50 {
		t.Fatalf("a large limit the user chose must not be clamped: got %d/%d", total, writers)
	}
	total, writers = NormalizeConcurrencyLimits(-3, -1)
	if total != 0 || writers != 0 {
		t.Fatalf("negative values read as no limit: got %d/%d", total, writers)
	}
}

// With no limit set, work that touches different paths all starts at once.
func TestAnUnlimitedSchedulerStartsEveryDisjointWriterAtOnce(t *testing.T) {
	root := testenv.TempDir(t)
	s := NewSubagentScheduler(0, 0)
	for i := range 40 {
		set, err := NormalizeWritePaths(root, []string{filepath.Join(root, "p", string(rune('a'+i%26))+string(rune('a'+i/26)))})
		if err != nil {
			t.Fatal(err)
		}
		release, err := s.Acquire(context.Background(), AcquireRequest{Writer: true, WritePaths: set, Nested: true})
		if err != nil {
			t.Fatalf("writer %d was held back with no limit set: %v", i+1, err)
		}
		defer release()
	}
}

func TestNormalizeWritePathsRejectsGlobsAndEscape(t *testing.T) {
	root := testenv.TempDir(t)
	if _, err := NormalizeWritePaths(root, []string{"docs/*.md"}); err == nil {
		t.Fatal("expected glob rejection")
	}
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	if _, err := NormalizeWritePaths(root, []string{outside}); err == nil {
		t.Fatal("expected workspace escape rejection")
	}
	// Relative file is fine even if it does not exist yet.
	got, err := NormalizeWritePaths(root, []string{"docs/01.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Paths) != 1 {
		t.Fatalf("paths = %v", got.Paths)
	}
	if !filepath.IsAbs(got.Paths[0]) {
		t.Fatalf("expected absolute path, got %q", got.Paths[0])
	}
}

func TestWritePathOverlapParentChildAndCase(t *testing.T) {
	root := testenv.TempDir(t)
	a, err := NormalizeWritePaths(root, []string{"docs"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NormalizeWritePaths(root, []string{"docs/01.md"})
	if err != nil {
		t.Fatal(err)
	}
	if !a.Overlaps(b) {
		t.Fatal("parent/child must overlap")
	}
	c, err := NormalizeWritePaths(root, []string{"other.md"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Overlaps(c) {
		t.Fatal("disjoint paths must not overlap")
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		// Case-variant of the same relative path should collide.
		upper := filepath.Join(root, "Docs")
		_ = os.MkdirAll(upper, 0o755)
		d, err := NormalizeWritePaths(root, []string{"Docs"})
		if err != nil {
			t.Fatal(err)
		}
		e, err := NormalizeWritePaths(root, []string{"docs"})
		if err != nil {
			t.Fatal(err)
		}
		if !d.Overlaps(e) {
			t.Fatal("case-equivalent paths must overlap on this platform")
		}
	}
	whole, err := WholeWorkspaceWriteClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	if !whole.Overlaps(c) {
		t.Fatal("whole workspace must overlap any path claim")
	}
}

func TestWritePathSetAllowsPath(t *testing.T) {
	root := testenv.TempDir(t)
	claim, err := NormalizeWritePaths(root, []string{"docs"})
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "docs", "x.md")
	if !claim.AllowsPath(inside) {
		t.Fatalf("should allow %s", inside)
	}
	outside := filepath.Join(root, "other.md")
	if claim.AllowsPath(outside) {
		t.Fatalf("should reject %s", outside)
	}
}

func TestWriteClaimRefusesNetworkPathBeforeAnyLookup(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("network paths are a Windows reading of a path")
	}
	root := testenv.TempDir(t)
	for _, p := range []string{`\\evil\share\x`, `//evil/share/x`, `\\127.0.0.1\C$\Windows\win.ini`} {
		if _, err := resolveWriteClaimPath(root, p); !errors.Is(err, fileutil.ErrNetworkPathOutsideScope) {
			t.Errorf("resolveWriteClaimPath(%q) = %v, want ErrNetworkPathOutsideScope", p, err)
		}
		if _, err := NormalizeWritePaths(root, []string{p}); err == nil {
			t.Errorf("NormalizeWritePaths(%q) accepted a network path", p)
		}
	}
}
