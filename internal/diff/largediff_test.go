package diff

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestLargeRewriteBoundedCost proves a full rewrite of a large file no longer
// pays O(N²): the edit-distance cap skips the line-by-line render, so memory and
// time stay bounded while the tallies and an omitted-diff marker still report the
// change. Before the cap this allocated ~565 MB for 3000 lines (≈6 GB at 10k).
func TestLargeRewriteBoundedCost(t *testing.T) {
	var oldB, newB strings.Builder
	const n = 6000
	for i := 0; i < n; i++ {
		fmt.Fprintf(&oldB, "old line %d\n", i)
		fmt.Fprintf(&newB, "totally different new line %d\n", i)
	}
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	start := time.Now()
	c := Build("big.txt", oldB.String(), newB.String(), Modify)
	elapsed := time.Since(start)
	runtime.ReadMemStats(&m1)
	allocMB := float64(m1.TotalAlloc-m0.TotalAlloc) / (1 << 20)
	t.Logf("2×%d-line rewrite: %v, %.1f MB, +%d/-%d", n, elapsed, allocMB, c.Added, c.Removed)

	if allocMB > 150 {
		t.Errorf("allocated %.1f MB — the edit-distance cap should bound this", allocMB)
	}
	if elapsed > time.Second {
		t.Errorf("took %v — should be bounded", elapsed)
	}
	if c.Added != n || c.Removed != n {
		t.Errorf("tallies wrong: +%d/-%d, want +%d/-%d", c.Added, c.Removed, n, n)
	}
	if !strings.Contains(c.Diff, "too large") {
		t.Errorf("expected an omitted-diff marker, got %q", c.Diff)
	}
}

// TestSmallEditOnLargeFileStillDiffs proves the cap doesn't punish the common
// case: a tiny edit in a big file converges far below the cap, so it keeps a real
// line-by-line diff regardless of file size.
func TestSmallEditOnLargeFileStillDiffs(t *testing.T) {
	var oldB strings.Builder
	const n = 8000
	for i := 0; i < n; i++ {
		fmt.Fprintf(&oldB, "line %d\n", i)
	}
	old := oldB.String()
	updated := strings.Replace(old, "line 4000\n", "line 4000 EDITED\n", 1)
	c := Build("big.txt", old, updated, Modify)
	if c.Added != 1 || c.Removed != 1 {
		t.Errorf("tallies = +%d/-%d, want +1/-1", c.Added, c.Removed)
	}
	if !strings.Contains(c.Diff, "line 4000 EDITED") || strings.Contains(c.Diff, "too large") {
		t.Errorf("small edit on a large file should keep a real diff, got %q", firstN(c.Diff, 200))
	}
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
