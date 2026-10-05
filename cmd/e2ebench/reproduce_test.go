package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWilsonAndNewcombeMatchKnownValues(t *testing.T) {
	lo, hi := wilson(0, 10)
	if lo != 0 && math.Abs(lo) > 1e-9 || math.Abs(hi-0.2775) > 0.001 {
		t.Fatalf("wilson(0,10) = (%.4f, %.4f), want (0, 0.2775)", lo, hi)
	}
	// Newcombe (1998), method 10: 56/70 vs 48/80 gives 0.0524 to 0.3339 for
	// p1 − p2; newcombe returns the interval for the second minus the first.
	lo, hi = newcombe(56, 70, 48, 80)
	if math.Abs(lo-(-0.3339)) > 0.0005 || math.Abs(hi-(-0.0524)) > 0.0005 {
		t.Fatalf("newcombe(56/70, 48/80) = (%.4f, %.4f), want (-0.3339, -0.0524)", lo, hi)
	}
}

func writeReport(t *testing.T, dir, name, capability string, pass int, n int) string {
	t.Helper()
	var rs []result
	for i := range n {
		r := result{task: task{ID: "t" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Prompt: "p"}, Passed: i < pass}
		r.Trajectory = &trajectorySummary{CapabilitiesHash: capability, BundleOutcome: "completed"}
		rs = append(rs, r)
	}
	b, err := json.Marshal(rs)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReproductionSection(t *testing.T) {
	dir := t.TempDir()
	a := writeReport(t, dir, "a.json", "sha256:aaaaaaaaaaaaaaaa", 40, 50)
	same := writeReport(t, dir, "b.json", "sha256:aaaaaaaaaaaaaaaa", 38, 50)
	got := reproductionSection(a, same)
	for _, want := range []string{"version: one version (aaaaaaaaaaaa)", "corpus: same 50 tasks", "within noise", "reproduces within the noise floor"} {
		if !strings.Contains(got, want) {
			t.Fatalf("same version report missing %q:\n%s", want, got)
		}
	}
	worse := writeReport(t, dir, "c.json", "sha256:aaaaaaaaaaaaaaaa", 5, 50)
	if got := reproductionSection(a, worse); !strings.Contains(got, "differs") || strings.Contains(got, "reproduces") {
		t.Fatalf("a 70pp drop read as noise:\n%s", got)
	}
	other := writeReport(t, dir, "d.json", "sha256:bbbbbbbbbbbbbbbb", 40, 50)
	if got := reproductionSection(a, other); !strings.Contains(got, "two versions") || strings.Contains(got, "reproduces") {
		t.Fatalf("two versions read as a reproduction:\n%s", got)
	}
}
