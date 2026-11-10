package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// stubScript builds a stand-in agent that records the --metrics path it was
// given, writes the metrics body it was built with to that path plus suffix,
// and exits with code. A non-empty suffix is what a killed child leaves: the
// in-flight snapshot and no final record.
func stubScript(t *testing.T, name, body, suffix string, code int) string {
	t.Helper()
	requireShellStub(t)
	script := fmt.Sprintf(`#!/bin/sh
metrics=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--metrics" ]; then metrics="$arg"; fi
  prev="$arg"
done
printf '%%s' "$metrics" > metrics-path.txt
printf '%%s' '%s' > "$metrics%s"
exit %d
`, body, suffix, code)
	path := filepath.Join(testenv.TempDir(t), name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

// recordedMetricsPath reads back the --metrics path the stub was handed.
func recordedMetricsPath(t *testing.T, work string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(work, "metrics-path.txt"))
	if err != nil {
		t.Fatalf("the stub did not record its metrics path: %v", err)
	}
	got := strings.TrimSpace(string(b))
	if got == "" {
		t.Fatal("the stub recorded an empty metrics path")
	}
	return got
}

// twoLegStub writes a stand-in whose legs differ: the first leaves only the
// in-flight snapshot, the second lands its final record. Both exit 0, which is
// what lets the run reach the second leg after the first one stopped short.
func twoLegStub(t *testing.T, name string) string {
	t.Helper()
	requireShellStub(t)
	script := `#!/bin/sh
metrics=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--metrics" ]; then metrics="$arg"; fi
  prev="$arg"
done
case "$metrics" in
  *-1.json)
    printf '%s' '{"prompt_tokens":1200,"completion_tokens":80,"steps":9,"cost":0.0123,"currency":"USD","complete":false}' > "$metrics.partial"
    ;;
  *)
    printf '%s' '{"prompt_tokens":300,"completion_tokens":20,"steps":2,"cost":0.001,"currency":"USD","complete":true}' > "$metrics"
    ;;
esac
exit 0
`
	path := filepath.Join(testenv.TempDir(t), name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

// A run that lands its final metrics record is untouched by the recovery
// path: it stays accounted, keeps its real numbers and is still not partial.
func TestSuiteHealthyRunKeepsItsFinalAccount(t *testing.T) {
	work := testenv.TempDir(t)
	tk := task{ID: "healthy-task", Prompt: "do the thing"}
	body := `{"prompt_tokens":900,"completion_tokens":60,"steps":4,"cost":0.02,` +
		`"currency":"USD","complete":true}`
	cfg := suiteConfig{bin: stubScript(t, "stub-ok.sh", body, "", 0)}
	r := result{task: tk}
	if err := runSegments(context.Background(), cfg, tk, work, "", nil, &r); err != nil {
		t.Fatalf("a healthy leg must not fail the run: %v", err)
	}
	stats := gatherSuiteStats([]result{r})
	if stats.unaccounted != 0 || stats.accounted != 1 || stats.partial != 0 {
		t.Fatalf("a healthy run changed category: unaccounted=%d accounted=%d partial=%d",
			stats.unaccounted, stats.accounted, stats.partial)
	}
	if stats.pTok != 900 || stats.cTok != 60 || stats.cost != 0.02 {
		t.Fatalf("a healthy run's numbers changed: tokens=%d/%d cost=%.4f",
			stats.pTok, stats.cTok, stats.cost)
	}
	if r.Partial || r.Unaccounted {
		t.Fatalf("a healthy run was flagged: partial=%v unaccounted=%v", r.Partial, r.Unaccounted)
	}
	// The metrics directory is disposable once the final record was read.
	dir := filepath.Dir(recordedMetricsPath(t, work))
	if dirExists(dir) {
		t.Fatalf("a healthy run left its metrics directory behind: %s", dir)
	}
}

// The snapshot a killed child leaves is the only evidence of what it spent, so
// recovering the numbers must not also destroy the file they came from.
func TestSuiteRunKeepsTheSnapshotItRecovered(t *testing.T) {
	work := testenv.TempDir(t)
	tk := task{ID: "killed-task", Prompt: "do the thing"}
	body := `{"prompt_tokens":1200,"completion_tokens":80,"steps":9,"cost":0.0123,` +
		`"currency":"USD","complete":false}`
	cfg := suiteConfig{bin: stubScript(t, "stub-partial.sh", body, ".partial", 137)}
	r := result{task: tk}
	if err := runSegments(context.Background(), cfg, tk, work, "", nil, &r); err == nil {
		t.Fatal("the stub exits 137; a leg that died must be reported as failed")
	}
	path := recordedMetricsPath(t, work)
	if fileExists(path) {
		t.Fatalf("a killed child cannot have written a final record: %s", path)
	}
	if !fileExists(path + ".partial") {
		t.Fatalf("the recovered snapshot was deleted with the metrics directory: %s.partial", path)
	}
	if r.PromptTokens != 1200 || r.CompletionTokens != 80 {
		t.Fatalf("the snapshot was kept but not recovered: tokens=%d/%d",
			r.PromptTokens, r.CompletionTokens)
	}
	// Recovering the numbers must not also mark the run unmeasured: the snapshot
	// is evidence, so the run stays accounted and Partial marks it a lower bound.
	if r.Unaccounted || !r.Partial {
		t.Fatalf("a recovered snapshot was not reported as accounted but partial: partial=%v unaccounted=%v",
			r.Partial, r.Unaccounted)
	}
}

// A lower bound belongs to the run, not to its last leg: a complete second leg
// cannot restore the tail a killed first leg stopped short of, so once any leg
// ends at a snapshot the whole run stays partial — and still counts.
func TestSuitePartialFirstLegMakesTheWholeRunALowerBound(t *testing.T) {
	work := testenv.TempDir(t)
	tk := task{ID: "two-leg-partial", Prompt: "do the thing", MaxSteps: 10}
	cfg := suiteConfig{bin: twoLegStub(t, "stub-two-leg.sh"), segments: 2}
	r := result{task: tk}
	if err := runSegments(context.Background(), cfg, tk, work, "", nil, &r); err != nil {
		t.Fatalf("both legs exit 0; the run must not fail: %v", err)
	}
	if r.Segments != 2 {
		t.Fatalf("the run did not take both legs: segments=%d", r.Segments)
	}
	// The second leg's completeness says nothing about the first leg's tail.
	if !r.Partial || r.Unaccounted {
		t.Fatalf("a complete last leg erased the first leg's lower bound:\n"+
			"  partial=%v unaccounted=%v, want partial=true unaccounted=false", r.Partial, r.Unaccounted)
	}
	// Partial bounds the account; it does not exclude it. Both legs are counted.
	if r.PromptTokens != 1500 || r.CompletionTokens != 100 {
		t.Fatalf("the legs' spend was not folded together: tokens=%d/%d, want 1500/100",
			r.PromptTokens, r.CompletionTokens)
	}
	stats := gatherSuiteStats([]result{r})
	if stats.accounted != 1 || stats.unaccounted != 0 || stats.partial != 1 || stats.pTok != 1500 {
		t.Fatalf("a two-leg partial run changed category:\n"+
			"  accounted=%d unaccounted=%d partial=%d pTok=%d, want 1/0/1/1500",
			stats.accounted, stats.unaccounted, stats.partial, stats.pTok)
	}
	if out := renderBody([]result{r}); !strings.Contains(out, "contributed partial accounting") {
		t.Fatalf("the run was counted but not disclosed as a lower bound:\n%s", out)
	}
}

// TestMeterAccountingSkipsAnUnaccountedRunWithTokens pins the guard: a run the
// suite marked unmeasured stays out of the ratio even when a harness number is
// present, so a run with no record is not priced.
func TestMeterAccountingSkipsAnUnaccountedRunWithTokens(t *testing.T) {
	r := result{
		task:        task{ID: "killed"},
		runMetrics:  runMetrics{PromptTokens: 100, CompletionTokens: 10},
		Unaccounted: true,
		Meter:       &meterUsage{PromptTokens: 1200, CompletionTokens: 80},
	}
	got := renderMeterAccounting([]result{r})
	if strings.Contains(got, "self-report divergence") {
		t.Fatalf("an unaccounted run with a harness number was priced as a self-report divergence:\n%s", got)
	}
}

// The same guard has to cover a lower bound: a Partial run's harness side stops
// at its last snapshot while the meter saw the whole leg, so pricing it reads as
// a divergence that is only the missing tail.
func TestMeterAccountingSkipsAPartialRunWithTokens(t *testing.T) {
	r := result{
		task:       task{ID: "lower-bound"},
		runMetrics: runMetrics{PromptTokens: 100, CompletionTokens: 10},
		Partial:    true,
		Meter:      &meterUsage{PromptTokens: 1200, CompletionTokens: 80},
	}
	got := renderMeterAccounting([]result{r})
	if strings.Contains(got, "self-report divergence") {
		t.Fatalf("a partial run's missing tail was priced as a self-report divergence:\n%s", got)
	}
	// Its meter tokens are still a real measurement, so the totals keep them.
	if !strings.Contains(got, "tokens 1,280") {
		t.Fatalf("skipping the ratio also dropped the run's metered tokens:\n%s", got)
	}
}
