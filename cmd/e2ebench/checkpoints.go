package main

import (
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// checkpoint is one workspace snapshot taken during a run, graded offline by
// the hidden grader after the run ends. Pass answers the question no final
// grade can: from which moment was the workspace already a correct answer.
type checkpoint struct {
	Seq       int   `json:"seq"`
	ElapsedMs int64 `json:"elapsed_ms"`
	Pass      bool  `json:"pass"`

	dir string
}

// snapshotter polls a running task's workdir and copies it on every observed
// content change. Torn copies of files mid-write are acceptable: they grade
// as failures, which is the truthful state of that instant.
type snapshotter struct {
	src, dst string
	start    time.Time
	stop     chan struct{}
	done     chan struct{}
	taken    []checkpoint
	lastSig  uint64
}

const snapshotPollInterval = 300 * time.Millisecond

func startSnapshotter(src, dst string, start time.Time) *snapshotter {
	s := &snapshotter{src: src, dst: dst, start: start, stop: make(chan struct{}), done: make(chan struct{})}
	s.lastSig = dirSignature(src)
	go s.run()
	return s
}

func (s *snapshotter) run() {
	defer close(s.done)
	ticker := time.NewTicker(snapshotPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.snapshotIfChanged()
		}
	}
}

func (s *snapshotter) snapshotIfChanged() {
	sig := dirSignature(s.src)
	if sig == s.lastSig {
		return
	}
	s.lastSig = sig
	elapsed := time.Since(s.start).Milliseconds()
	dir := filepath.Join(s.dst, fmt.Sprintf("%03d-%dms", len(s.taken)+1, elapsed))
	if err := copyDir(s.src, dir); err != nil {
		return
	}
	os.Remove(filepath.Join(dir, ".run-metrics.json"))
	s.taken = append(s.taken, checkpoint{Seq: len(s.taken) + 1, ElapsedMs: elapsed, dir: dir})
}

// halt stops polling, takes one final snapshot if the tail changed after the
// last tick, and returns everything captured.
func (s *snapshotter) halt() []checkpoint {
	close(s.stop)
	<-s.done
	s.snapshotIfChanged()
	return s.taken
}

// dirSignature hashes the workdir's shape (path, size, mtime). The agent's
// metrics sidecar updates every turn without touching the workspace, so it
// is excluded — as are bytecode caches, which no grader reads.
func dirSignature(root string) uint64 {
	h := fnv.New64a()
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		if name == ".run-metrics.json" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		fmt.Fprintf(h, "%s|%d|%d\n", rel, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return h.Sum64()
}

// gradeCheckpoints runs the hidden grader over every snapshot, oldest first,
// after the agent is gone — the agent never sees a verdict.
func gradeCheckpoints(checkpoints []checkpoint, taskDir string) []checkpoint {
	for i := range checkpoints {
		checkpoints[i].Pass = grade(checkpoints[i].dir, taskDir)
	}
	return checkpoints
}

// firstCorrect returns the elapsed ms of the earliest passing snapshot (0 if
// none) and whether a passing state was later broken (a snapshot passed but
// the final workspace failed).
func firstCorrect(checkpoints []checkpoint, finalPassed bool) (firstMs int64, solvedThenBroken bool) {
	anyPassed := false
	for _, cp := range checkpoints {
		if cp.Pass {
			anyPassed = true
			if firstMs == 0 {
				firstMs = cp.ElapsedMs
			}
		}
	}
	return firstMs, anyPassed && !finalPassed
}

// solveProfile buckets a checkpointed run into the "why slow" cases that
// demand opposite optimizations: early_correct = termination/verification
// overhead; late_correct = exploration/decision efficiency; never_correct =
// capability (chasing latency is pointless); solved_then_broke = a passing
// state destroyed afterwards. Empty when the run took no checkpoints.
func solveProfile(r result) string {
	if len(r.Checkpoints) == 0 {
		return ""
	}
	switch {
	case r.SolvedThenBroken:
		return "solved_then_broke"
	case !r.Passed:
		return "never_correct"
	case r.FirstCorrectMs == 0:
		return "late_correct" // only the final state passed
	case r.PostSolveWasteMs >= 5000 && r.PostSolveWasteMs*3 >= r.WallMs:
		return "early_correct"
	default:
		return "late_correct"
	}
}

// renderSolveProfiles is the triage line: how many runs fall into each case,
// with the early-correct bucket's median waste — the directly recoverable tail.
func renderSolveProfiles(results []result) string {
	counts := map[string]int{}
	var earlyWaste []int64
	for _, r := range results {
		profile := solveProfile(r)
		if profile == "" {
			continue
		}
		counts[profile]++
		if profile == "early_correct" {
			earlyWaste = append(earlyWaste, r.PostSolveWasteMs)
		}
	}
	if len(counts) == 0 {
		return ""
	}
	line := "**Solve profile**: "
	parts := []string{}
	regressed, withCorrect := 0, 0
	var roundsBefore, roundsAfter, verifyAfter, mutationsBefore []int64
	for _, r := range results {
		if len(r.Checkpoints) == 0 {
			continue
		}
		if r.FirstCorrectMs > 0 {
			withCorrect++
			roundsBefore = append(roundsBefore, int64(r.RoundsBeforeCorrect))
			roundsAfter = append(roundsAfter, int64(r.RoundsAfterCorrect))
			verifyAfter = append(verifyAfter, int64(r.VerifyAfterCorrect))
			mutationsBefore = append(mutationsBefore, int64(r.MutationsBeforeCorrect))
		}
		if r.RegressedAfterCorrect {
			regressed++
		}
	}
	for _, profile := range []string{"early_correct", "late_correct", "never_correct", "solved_then_broke"} {
		if counts[profile] == 0 {
			continue
		}
		part := fmt.Sprintf("**%s** %d", profile, counts[profile])
		if profile == "early_correct" {
			part += fmt.Sprintf(" (median waste %s)", dur(median(earlyWaste)))
		}
		parts = append(parts, part)
	}
	line += joinParts(parts)
	stoppable, harmful, past := 0, 0, []int64{}
	for _, r := range results {
		if r.StopEval == nil {
			continue
		}
		if r.StopEval.FirstStoppableRound > 0 {
			stoppable++
			past = append(past, int64(r.StopEval.ContinuationsPast))
		}
		harmful += r.StopEval.HarmfulContinuation
	}
	if stoppable > 0 {
		line += fmt.Sprintf("\n\n**Stop policy**: stoppable before final in %d runs · **continuations past stoppable** median %d rounds · **harmful continuations** %d", stoppable, median(past), harmful)
	}
	if diagnosis := renderDiagnosis(results); diagnosis != "" {
		line += "\n\n" + strings.TrimSuffix(diagnosis, "\n\n")
	}
	if withCorrect > 0 {
		var ttfum []int64
		for _, r := range results {
			if r.FirstUsefulMs > 0 {
				ttfum = append(ttfum, r.FirstUsefulMs)
			}
		}
		line += fmt.Sprintf("\n\n**Correct boundary** (medians): **mutations before** %d · **rounds before** %d · **rounds after** %d · **verifications after** %d · **regression-after-correct** %s",
			median(mutationsBefore), median(roundsBefore), median(roundsAfter), median(verifyAfter),
			pct(regressed, withCorrect))
		if len(ttfum) > 0 {
			line += fmt.Sprintf(" · **TTFUM median** %s", dur(median(ttfum)))
		}
	}
	return line + "\n\n"
}

func joinParts(parts []string) string {
	return strings.Join(parts, " · ")
}

// mutationsBeforeCorrect counts the workspace states tried before the first
// passing one — how many edits it took to find the correct patch.
func mutationsBeforeCorrect(checkpoints []checkpoint) int {
	for i, cp := range checkpoints {
		if cp.Pass {
			return i
		}
	}
	return len(checkpoints)
}

// regressedAfterCorrect reports a PASS followed by a later FAIL — the agent
// kept "improving" a correct answer and broke it, even if it repaired the
// damage before finishing.
func regressedAfterCorrect(checkpoints []checkpoint) bool {
	seenPass := false
	for _, cp := range checkpoints {
		if cp.Pass {
			seenPass = true
		} else if seenPass {
			return true
		}
	}
	return false
}

// stopEval is the counterfactual-stop readout: had the run stopped at the
// end of round N, would the grader have passed? Derived by aligning round
// boundaries with the checkpoint grid — no agent participation.
type stopEval struct {
	Curve               []bool `json:"curve"`                           // round-end verdicts, 1-based rounds
	FirstStoppableRound int    `json:"first_stoppable_round,omitempty"` // 0 = never
	ContinuationsPast   int    `json:"continuations_past,omitempty"`    // rounds run after first stoppable
	HarmfulContinuation int    `json:"harmful_continuations,omitempty"` // PASS→FAIL round transitions
}

// computeStopEval grades each round's end state as the last checkpoint at or
// before that boundary; a boundary before any snapshot is the seed (fail).
func computeStopEval(checkpoints []checkpoint, roundEndElapsedMs []int64) *stopEval {
	if len(checkpoints) == 0 || len(roundEndElapsedMs) == 0 {
		return nil
	}
	eval := &stopEval{}
	prev := false
	for i, end := range roundEndElapsedMs {
		pass := false
		for _, cp := range checkpoints {
			if cp.ElapsedMs <= end {
				pass = cp.Pass
			} else {
				break
			}
		}
		eval.Curve = append(eval.Curve, pass)
		if pass && eval.FirstStoppableRound == 0 {
			eval.FirstStoppableRound = i + 1
		}
		if prev && !pass {
			eval.HarmfulContinuation++
		}
		prev = pass
	}
	if eval.FirstStoppableRound > 0 {
		eval.ContinuationsPast = len(eval.Curve) - eval.FirstStoppableRound
	}
	return eval
}

// firstUsefulMutation approximates TTFUM — when part of the final solution
// first appeared: the earliest checkpoint in which any file that differs
// between seed and final already carries its exact final content. Cosmetic
// late edits make this an overestimate; that bias is stated, not hidden.
func firstUsefulMutation(checkpoints []checkpoint, seedDir, finalDir string) int64 {
	solution := solutionFiles(seedDir, finalDir)
	if len(solution) == 0 {
		return 0
	}
	for _, cp := range checkpoints {
		for rel, want := range solution {
			got, err := os.ReadFile(filepath.Join(cp.dir, rel))
			if err == nil && string(got) == want {
				return cp.ElapsedMs
			}
		}
	}
	return 0
}

// solutionFiles maps relative path → final content for every file the run
// created or changed; harness artifacts are not part of anyone's solution.
func solutionFiles(seedDir, finalDir string) map[string]string {
	skip := map[string]bool{".run-metrics.json": true, "verify.sh": true}
	out := map[string]string{}
	_ = filepath.WalkDir(finalDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(finalDir, path)
		if skip[filepath.Base(rel)] {
			return nil
		}
		final, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		seed, err := os.ReadFile(filepath.Join(seedDir, rel))
		if err == nil && string(seed) == string(final) {
			return nil // unchanged from seed: not part of the solution
		}
		out[rel] = string(final)
		return nil
	})
	return out
}
