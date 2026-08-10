package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTaskVerify(t *testing.T, taskDir, script string) {
	t.Helper()
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "verify.sh"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGradeCheckpointsFindsEarliestCorrectState(t *testing.T) {
	taskDir := filepath.Join(t.TempDir(), "task")
	writeTaskVerify(t, taskDir, "#!/usr/bin/env bash\ngrep -q done answer.txt\n")

	snaps := t.TempDir()
	mk := func(seq int, elapsed int64, content string) checkpoint {
		dir := filepath.Join(snaps, filepath.Base(strings.ReplaceAll(content, " ", "-"))+"-cp")
		dir = filepath.Join(snaps, filepath.Base(dir)+"-"+strings.ReplaceAll(content, " ", "_"))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "answer.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return checkpoint{Seq: seq, ElapsedMs: elapsed, dir: dir}
	}
	checkpoints := gradeCheckpoints([]checkpoint{
		mk(1, 18_000, "not yet"),
		mk(2, 63_000, "done"),
		mk(3, 128_000, "done and polished"),
	}, taskDir)
	if checkpoints[0].Pass || !checkpoints[1].Pass || !checkpoints[2].Pass {
		t.Fatalf("pass flags = %+v", checkpoints)
	}

	firstMs, broke := firstCorrect(checkpoints, true)
	if firstMs != 63_000 || broke {
		t.Fatalf("firstCorrect = %d, %v; want 63000, false", firstMs, broke)
	}

	// A run whose final state failed after a passing snapshot is the
	// solved-then-broke alarm.
	firstMs, broke = firstCorrect(checkpoints, false)
	if firstMs != 63_000 || !broke {
		t.Fatalf("solved-then-broke = %d, %v; want 63000, true", firstMs, broke)
	}

	if ms, broke := firstCorrect([]checkpoint{{Seq: 1, ElapsedMs: 5, dir: snaps}}, true); ms != 0 || broke {
		t.Fatalf("no passing snapshot must yield 0/false, got %d/%v", ms, broke)
	}
}

func TestSnapshotterCapturesWorkspaceChanges(t *testing.T) {
	work := t.TempDir()
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "code.py"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	polls := make(chan time.Time)
	acks := make(chan struct{})
	snap := startSnapshotterWithPoll(work, dst, time.Now(), polls, acks)
	poll := func() {
		t.Helper()
		polls <- time.Time{}
		<-acks
	}
	poll()
	// The metrics sidecar updating must not trigger a snapshot on its own.
	if err := os.WriteFile(filepath.Join(work, ".run-metrics.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	poll()
	if err := os.WriteFile(filepath.Join(work, "code.py"), []byte("v2 changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	poll()
	taken := snap.halt()

	if len(taken) != 1 {
		t.Fatalf("snapshots = %d (%+v), want exactly 1 (the code change; metrics writes excluded)", len(taken), taken)
	}
	data, err := os.ReadFile(filepath.Join(taken[0].dir, "code.py"))
	if err != nil || string(data) != "v2 changed" {
		t.Fatalf("snapshot content = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(taken[0].dir, ".run-metrics.json")); !os.IsNotExist(err) {
		t.Fatal("metrics sidecar must be stripped from snapshots")
	}
}

func TestKPILineIncludesTTFCS(t *testing.T) {
	r := result{task: task{ID: "a"}, Passed: true, WallMs: 142_000, Attempt: 1, TTCSMs: 142_000}
	r.FirstCorrectMs = 63_000
	r.PostSolveWasteMs = 79_000
	got := renderBody([]result{r})
	for _, want := range []string{
		"**TTFCS median** 1m03s",
		"**post-solve waste median** 1m19s",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("KPI line missing %q:\n%s", want, got)
		}
	}
}

func TestSolveProfileTriage(t *testing.T) {
	cp := []checkpoint{{Seq: 1, ElapsedMs: 1000}}
	early := result{Passed: true, WallMs: 140_000, FirstCorrectMs: 55_000, PostSolveWasteMs: 85_000, Checkpoints: cp}
	late := result{Passed: true, WallMs: 140_000, FirstCorrectMs: 132_000, PostSolveWasteMs: 8_000, Checkpoints: cp}
	never := result{Passed: false, Checkpoints: cp}
	broke := result{Passed: false, SolvedThenBroken: true, Checkpoints: cp}
	finalOnly := result{Passed: true, WallMs: 30_000, Checkpoints: cp}
	off := result{Passed: true}

	for want, r := range map[string]result{
		"early_correct": early, "late_correct": late, "never_correct": never,
		"solved_then_broke": broke, "": off,
	} {
		if got := solveProfile(r); got != want {
			t.Fatalf("solveProfile = %q, want %q", got, want)
		}
	}
	if got := solveProfile(finalOnly); got != "late_correct" {
		t.Fatalf("final-only pass = %q, want late_correct", got)
	}

	line := renderSolveProfiles([]result{early, late, never, broke})
	for _, want := range []string{
		"**early_correct** 1 (median waste 1m25s)",
		"**late_correct** 1",
		"**never_correct** 1",
		"**solved_then_broke** 1",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("solve profile line missing %q:\n%s", want, line)
		}
	}
	if renderSolveProfiles([]result{off}) != "" {
		t.Fatal("uncheckpointed suites must not render the line")
	}
}

func TestCorrectBoundaryMetrics(t *testing.T) {
	cps := []checkpoint{
		{Seq: 1, ElapsedMs: 10, Pass: false},
		{Seq: 2, ElapsedMs: 20, Pass: false},
		{Seq: 3, ElapsedMs: 30, Pass: true},
		{Seq: 4, ElapsedMs: 40, Pass: false},
		{Seq: 5, ElapsedMs: 50, Pass: true},
	}
	if got := mutationsBeforeCorrect(cps); got != 2 {
		t.Fatalf("mutations before correct = %d, want 2", got)
	}
	if !regressedAfterCorrect(cps) {
		t.Fatal("PASS→FAIL→PASS must count as a regression even though it was repaired")
	}
	if regressedAfterCorrect(cps[:3]) {
		t.Fatal("no regression before the first failure-after-pass")
	}
	if got := mutationsBeforeCorrect(cps[:2]); got != 2 {
		t.Fatalf("all-failing run: mutations = %d, want len", got)
	}
}

func TestRoundsSplitAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "split.trajectory.jsonl")
	lines := []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"seq":2,"ts":2000,"event":{"kind":"tool_dispatch","tool":{"id":"a","name":"write_file"}}}`,
		`{"seq":3,"ts":2100,"event":{"kind":"tool_result","tool":{"id":"a","name":"write_file","durationMs":100}}}`,
		`{"seq":4,"ts":3000,"event":{"kind":"tool_dispatch","tool":{"id":"b","name":"bash"}}}`,
		`{"seq":5,"ts":3200,"event":{"kind":"tool_result","tool":{"id":"b","name":"bash","readOnly":true,"durationMs":200,"execution":{"verification":"passed"}}}}`,
		`{"seq":6,"ts":5000,"event":{"kind":"tool_dispatch","tool":{"id":"c","name":"bash"}}}`,
		`{"seq":7,"ts":5200,"event":{"kind":"tool_result","tool":{"id":"c","name":"bash","readOnly":true,"durationMs":200,"execution":{"verification":"passed"}}}}`,
		`{"seq":8,"ts":6000,"event":{"kind":"tool_dispatch","tool":{"id":"d","name":"read_file","readOnly":true}}}`,
		`{"seq":9,"ts":6100,"event":{"kind":"tool_result","tool":{"id":"d","name":"read_file","readOnly":true,"durationMs":100}}}`,
		`{"seq":10,"ts":7000,"event":{"kind":"turn_done"}}`,
	}
	if err := writeLines(path, lines); err != nil {
		t.Fatal(err)
	}
	split := splitAtCorrect(path, 4000)
	if split.RoundsBefore != 2 || split.RoundsAfter != 2 || split.VerifyAfter != 1 {
		t.Fatalf("split = %+v, want 2 rounds before, 2 after, 1 verification after", split)
	}
	if split.CallsBefore != 2 || split.CallsAfter != 2 {
		t.Fatalf("calls = %d/%d, want 2/2", split.CallsBefore, split.CallsAfter)
	}
	if split.MutationsAfter != 0 {
		t.Fatalf("read-only tail must count no mutations, got %d", split.MutationsAfter)
	}
}

func TestComputeStopEvalCurveAndHarmfulContinuations(t *testing.T) {
	cps := []checkpoint{
		{Seq: 1, ElapsedMs: 8_000, Pass: false},
		{Seq: 2, ElapsedMs: 18_000, Pass: true},
		{Seq: 3, ElapsedMs: 28_000, Pass: true},
		{Seq: 4, ElapsedMs: 38_000, Pass: false}, // the "improvement" that broke it
		{Seq: 5, ElapsedMs: 48_000, Pass: true},
	}
	rounds := []int64{10_000, 20_000, 30_000, 40_000, 50_000}
	eval := computeStopEval(cps, rounds)
	want := []bool{false, true, true, false, true}
	for i, pass := range want {
		if eval.Curve[i] != pass {
			t.Fatalf("curve = %v, want %v", eval.Curve, want)
		}
	}
	if eval.FirstStoppableRound != 2 {
		t.Fatalf("first stoppable = %d, want 2", eval.FirstStoppableRound)
	}
	if eval.HarmfulContinuation != 1 {
		t.Fatalf("harmful continuations = %d, want 1 (round 4 destroyed a passing state)", eval.HarmfulContinuation)
	}
	if eval.ContinuationsPast != 3 {
		t.Fatalf("continuations past stoppable = %d, want 3", eval.ContinuationsPast)
	}

	if computeStopEval(nil, rounds) != nil || computeStopEval(cps, nil) != nil {
		t.Fatal("missing inputs must yield no eval")
	}
	// A boundary before any snapshot grades as the seed: fail.
	early := computeStopEval(cps, []int64{1_000})
	if early.Curve[0] || early.FirstStoppableRound != 0 {
		t.Fatalf("pre-snapshot boundary must fail: %+v", early)
	}
}

func TestOverthinkingDamageRateInKPIAndCompare(t *testing.T) {
	damaged := result{task: task{ID: "a"}, Passed: true, WallMs: 60_000, Attempt: 1, TTCSMs: 60_000}
	damaged.FirstCorrectMs = 20_000
	damaged.PostSolveWasteMs = 40_000
	damaged.RegressedAfterCorrect = true
	clean := result{task: task{ID: "b"}, Passed: true, WallMs: 30_000, Attempt: 1, TTCSMs: 30_000}
	clean.FirstCorrectMs = 25_000
	clean.PostSolveWasteMs = 5_000

	got := renderBody([]result{damaged, clean})
	if !strings.Contains(got, "**overthinking damage** 50%") {
		t.Fatalf("KPI line missing damage rate:\n%s", got)
	}
}

func TestFirstUsefulMutationApproximatesTTFUM(t *testing.T) {
	seed := t.TempDir()
	final := t.TempDir()
	snaps := t.TempDir()
	write := func(dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(seed, "util.py", "v0")
	write(seed, "keep.py", "same")
	write(final, "util.py", "final fix")
	write(final, "keep.py", "same")
	write(final, "helper.py", "created")
	write(final, "verify.sh", "grader")

	mk := func(seq int, elapsed int64, utilContent string) checkpoint {
		dir := filepath.Join(snaps, fmt.Sprintf("%03d", seq))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		write(dir, "util.py", utilContent)
		return checkpoint{Seq: seq, ElapsedMs: elapsed, dir: dir}
	}
	cps := []checkpoint{
		mk(1, 10_000, "wrong attempt"),
		mk(2, 20_000, "final fix"), // part of the final solution appears
		mk(3, 30_000, "final fix"),
	}
	if got := firstUsefulMutation(cps, seed, final); got != 20_000 {
		t.Fatalf("TTFUM = %d, want 20000", got)
	}

	// A created file reaching its final content also counts.
	write(filepath.Join(snaps, "001"), "helper.py", "created")
	if got := firstUsefulMutation(cps, seed, final); got != 10_000 {
		t.Fatalf("TTFUM with created file = %d, want 10000", got)
	}

	// Unchanged and harness files are never solution files.
	files := solutionFiles(seed, final)
	if _, ok := files["keep.py"]; ok {
		t.Fatal("unchanged file counted as solution")
	}
	if _, ok := files["verify.sh"]; ok {
		t.Fatal("grader counted as solution")
	}
	if len(files) != 2 {
		t.Fatalf("solution files = %v", files)
	}
}

func TestDiagnosisAppliesThePriorityTree(t *testing.T) {
	cps := []checkpoint{{Seq: 1, ElapsedMs: 1000}}
	wasteful := result{task: task{ID: "a"}, Passed: true, WallMs: 100_000, Checkpoints: cps,
		FirstCorrectMs: 30_000, PostSolveWasteMs: 70_000, FirstUsefulMs: 20_000}
	explorer := result{task: task{ID: "b"}, Passed: true, WallMs: 100_000, Checkpoints: cps,
		FirstCorrectMs: 90_000, PostSolveWasteMs: 10_000, FirstUsefulMs: 85_000}
	never := result{task: task{ID: "c"}, Checkpoints: cps}
	broken := result{task: task{ID: "d"}, Passed: true, WallMs: 50_000, Checkpoints: cps,
		FirstCorrectMs: 10_000, PostSolveWasteMs: 5_000, RegressedAfterCorrect: true}

	if got := renderDiagnosis([]result{wasteful}); !strings.Contains(got, "post-solve waste dominates") {
		t.Fatalf("waste verdict missing: %s", got)
	}
	if got := renderDiagnosis([]result{explorer}); !strings.Contains(got, "exploration dominates") {
		t.Fatalf("exploration verdict missing: %s", got)
	}
	if got := renderDiagnosis([]result{never, never, wasteful}); !strings.Contains(got, "never-correct dominates") {
		t.Fatalf("capability verdict must outrank latency: %s", got)
	}
	if got := renderDiagnosis([]result{broken, wasteful, wasteful}); !strings.Contains(got, "correct→incorrect regressions") {
		t.Fatalf("damage verdict must outrank waste-vs-exploration: %s", got)
	}
	if renderDiagnosis([]result{{task: task{ID: "x"}}}) != "" {
		t.Fatal("uncheckpointed suites carry no diagnosis")
	}
}
