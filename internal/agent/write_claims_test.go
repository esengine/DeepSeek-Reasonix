package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNormalizeConcurrencyLimitsDefaults(t *testing.T) {
	total, writers := NormalizeConcurrencyLimits(0, 0)
	if total != DefaultMaxSubagentConcurrency || writers != DefaultMaxParallelWriters {
		t.Fatalf("got %d/%d, want %d/%d", total, writers, DefaultMaxSubagentConcurrency, DefaultMaxParallelWriters)
	}
	total, writers = NormalizeConcurrencyLimits(10, 10)
	if total != 10 || writers != 10 {
		t.Fatalf("got %d/%d, want 10/10", total, writers)
	}
	total, writers = NormalizeConcurrencyLimits(4, 10)
	if total != 4 || writers != 4 {
		t.Fatalf("writers must not exceed total: got %d/%d", total, writers)
	}
	total, writers = NormalizeConcurrencyLimits(100, 50)
	if total != MaxSubagentConcurrencyLimit || writers != MaxSubagentConcurrencyLimit {
		t.Fatalf("clamp: got %d/%d", total, writers)
	}
}

func TestNormalizeWritePathsRejectsGlobsAndEscape(t *testing.T) {
	root := t.TempDir()
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
	root := t.TempDir()
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

func TestValidateNonOverlappingWriteClaims(t *testing.T) {
	root := t.TempDir()
	a, _ := NormalizeWritePaths(root, []string{"a.md"})
	b, _ := NormalizeWritePaths(root, []string{"b.md"})
	if err := ValidateNonOverlappingWriteClaims([]WritePathSet{a, b}); err != nil {
		t.Fatal(err)
	}
	c, _ := NormalizeWritePaths(root, []string{"a.md"})
	if err := ValidateNonOverlappingWriteClaims([]WritePathSet{a, c}); err == nil {
		t.Fatal("expected conflict")
	}
}

func TestWritePathSetAllowsPath(t *testing.T) {
	root := t.TempDir()
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
