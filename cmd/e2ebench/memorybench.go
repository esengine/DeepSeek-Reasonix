// MemoryBench support: per-task memory-store seeding, the memory-off
// counterfactual arm, recall/marker extraction from trajectories, and the
// paired memory-utility readout. The core KPI is Task Pass(memory on) minus
// Task Pass(memory off) on identical tasks — retrieval that looks relevant
// but does not move task outcomes counts as overhead, not as recall quality.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/config"
)

// seedTaskMemory populates stateHome's memory store from the task's memory/
// directory (memory/project/*.md and memory/global/*.md). Tasks without seeds
// leave the store empty, which is the ordinary arm.
func seedTaskMemory(taskDir, work, stateHome string) error {
	seeds := filepath.Join(taskDir, "memory")
	if _, err := os.Stat(seeds); err != nil {
		return nil
	}
	absWork, err := filepath.Abs(work)
	if err != nil {
		return err
	}
	// The child derives its project slug from Getwd, which returns the
	// symlink-resolved path (/private/var vs /var on macOS); seed under the
	// same identity or the store lands in a directory nobody reads.
	if resolved, rErr := filepath.EvalSymlinks(absWork); rErr == nil {
		absWork = resolved
	}
	pairs := [][2]string{
		{filepath.Join(seeds, "project"), filepath.Join(stateHome, "projects", config.WorkspaceSlug(absWork), "memory")},
		{filepath.Join(seeds, "global"), filepath.Join(stateHome, "memory", "global")},
	}
	for _, pair := range pairs {
		if _, err := os.Stat(pair[0]); err != nil {
			continue
		}
		if err := os.MkdirAll(pair[1], 0o755); err != nil {
			return err
		}
		if err := copyDir(pair[0], pair[1]); err != nil {
			return err
		}
	}
	return nil
}

// taskExperimentEnv assembles one run's experiment environment: a state root of
// its own, the policy arm, and any seeded memory inside it. Per task on purpose:
// a shared root leaves each finished task's event stream where the next one can
// grep for it, and files benchmark sessions in the operator's real store.
// cleanup drops the root; note reports a failure without aborting the run.
func taskExperimentEnv(cfg suiteConfig, t task, work string) (env []string, cleanup func(), note string) {
	cleanup = func() {}
	memoryOff := cfg.policy == "memory-off"
	if memoryOff {
		env = append(env, "REASONIX_EXPERIMENT_NO_MEMORY=1")
	}
	stateHome, err := makeStateHome(work)
	if err != nil {
		return env, cleanup, "state root: " + err.Error()
	}
	cleanup = func() { _ = os.RemoveAll(stateHome) }
	env = append(env, "REASONIX_STATE_HOME="+stateHome)
	// A run that inherits the host temp root reads what earlier runs left in
	// it. A no-solution task that finds its missing dependency there — a .pyc
	// some previous run compiled — has stopped being a no-solution task.
	tmpDir, err := os.MkdirTemp("", "e2ebench-tmp-")
	if err != nil {
		return env, cleanup, "temp root: " + err.Error()
	}
	cleanup = func() { _ = os.RemoveAll(stateHome); _ = os.RemoveAll(tmpDir) }
	env = append(env, "TMPDIR="+tmpDir)
	// The off arm's treatment is that no memory exists. Hiding only the tool
	// leaves the seeded files on disk for a shell command to read.
	if memoryOff {
		return env, cleanup, ""
	}
	if err := seedTaskMemory(t.dir, work, stateHome); err != nil {
		return env, cleanup, "memory seed: " + err.Error()
	}
	return env, cleanup, ""
}

// benchStateBase is the directory every run's state root is made in. It is
// outside the temp root holding the workdirs: a sibling of the workdir is one
// `cd ..` and a find away from a run hunting for its seeded answers.
var benchStateBase = func() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "reasonix-bench-state"), nil
}

// makeStateHome creates one run's state root. Its name is random and carries
// neither the task id nor any part of the workdir's name, so a run searching
// the disk for its own name does not find the directory holding its session.
func makeStateHome(work string) (string, error) {
	base, err := benchStateBase()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	home, err := os.MkdirTemp(base, "")
	if err != nil {
		return "", err
	}
	if within(resolvedPath(filepath.Dir(work)), resolvedPath(home)) {
		_ = os.RemoveAll(home)
		return "", fmt.Errorf("%s is inside the workdir's parent %s", home, filepath.Dir(work))
	}
	return home, nil
}

func resolvedPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return p
}

// within reports whether path is dir or lies below it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// applyMemoryStats folds one trajectory's recall behavior into the result row.
func applyMemoryStats(r *result, trajPath string, t task) {
	stats := scanMemoryRecall(trajPath, t.MemoryMarkers, t.MemoryMarkersPrefix)
	r.MemoryRecallEvents, r.MemoryRecallHits = stats.RecallEvents, stats.RecallHits
	r.MemoryRecallChars, r.MemorySuppressed = stats.RecallChars, stats.Suppressed
	r.MemoryMarkersUsed, r.MemoryShadowAgree = stats.MarkersUsed, stats.ShadowAgree
}

// memoryRunStats is what one trajectory reveals about recall behavior.
type memoryRunStats struct {
	RecallEvents int // user turns where automatic recall ran and injected facts
	RecallHits   int
	RecallChars  int
	Suppressed   int // recall decisions that stayed silent
	MarkersUsed  int // task markers seen in tool args or answer text after recall
	ShadowAgree  int // recall events where the V2 shadow's top hit matched production's
}

// scanMemoryRecall extracts recall decisions and point-of-use evidence: a
// marker (a unique token planted in a seeded fact body) counts as used only
// when it appears in tool arguments or answer text AFTER a recall injected
// facts — the fact reached the decision path, not just the ranking.
func scanMemoryRecall(path string, markers []string, markersInPrefix bool) memoryRunStats {
	var stats memoryRunStats
	f, err := os.Open(path)
	if err != nil {
		return stats
	}
	defer f.Close()
	type record struct {
		MemoryRecall *struct {
			Hits       []struct{ ID string } `json:"hits"`
			UsedChars  int                   `json:"used_chars"`
			Suppressed string                `json:"suppressed"`
			ShadowHits []struct{ ID string } `json:"shadow_hits"`
		} `json:"memory_recall"`
		Event *struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
			Tool *struct {
				Args string `json:"args"`
			} `json:"tool"`
		} `json:"event"`
	}
	used := make(map[string]bool, len(markers))
	// Pinned facts arrive via the stable prefix, before any recall: their
	// markers count from the first record.
	recalled := markersInPrefix
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		var rec record
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}
		if mr := rec.MemoryRecall; mr != nil {
			if len(mr.Hits) > 0 {
				stats.RecallEvents++
				stats.RecallHits += len(mr.Hits)
				stats.RecallChars += mr.UsedChars
				recalled = true
				if len(mr.ShadowHits) > 0 && mr.ShadowHits[0].ID == mr.Hits[0].ID {
					stats.ShadowAgree++
				}
			} else if mr.Suppressed != "" {
				stats.Suppressed++
			}
		}
		if !recalled || rec.Event == nil {
			continue
		}
		var haystack string
		if rec.Event.Tool != nil {
			haystack = rec.Event.Tool.Args
		} else if rec.Event.Kind == "text" || rec.Event.Kind == "message" {
			haystack = rec.Event.Text
		}
		if haystack == "" {
			continue
		}
		for _, marker := range markers {
			if !used[marker] && strings.Contains(haystack, marker) {
				used[marker] = true
			}
		}
	}
	stats.MarkersUsed = len(used)
	return stats
}

// renderMemoryShadow aggregates recall behavior across a suite run; empty when
// no run recalled anything and no task planted markers.
func renderMemoryShadow(results []result) string {
	runs, recallRuns, hits, chars, suppressed, markersUsed, markersTotal := 0, 0, 0, 0, 0, 0, 0
	for _, r := range results {
		runs++
		if r.MemoryRecallEvents > 0 {
			recallRuns++
		}
		hits += r.MemoryRecallHits
		chars += r.MemoryRecallChars
		suppressed += r.MemorySuppressed
		markersUsed += r.MemoryMarkersUsed
		markersTotal += len(r.MemoryMarkers)
	}
	if hits == 0 && markersTotal == 0 {
		return ""
	}
	shadowAgree := 0
	for _, r := range results {
		shadowAgree += r.MemoryShadowAgree
	}
	line := fmt.Sprintf("**Memory shadow** (%d runs): **recall fired** in %d runs · **hits** %d · **injected chars** %d",
		runs, recallRuns, hits, chars)
	if recallRuns > 0 {
		line += fmt.Sprintf(" · **V2 top1 agree** %d/%d", shadowAgree, recallRuns)
	}
	if markersTotal > 0 {
		line += fmt.Sprintf(" · **point-of-use** %d/%d markers", markersUsed, markersTotal)
	}
	if suppressed > 0 {
		line += fmt.Sprintf(" · suppressed %d", suppressed)
	}
	return line + "\n\n"
}

// memoryUtilitySection is the paired counterfactual readout for two arms of
// the same suite. The on-arm is whichever side recalled; pairing by task ID
// cancels task difficulty, so the delta is memory's contribution.
func memoryUtilitySection(pathA, pathB string) string {
	a, errA := loadResults(pathA)
	b, errB := loadResults(pathB)
	if errA != nil || errB != nil {
		return ""
	}
	on, off := a, b
	if recallTotal(b) > recallTotal(a) {
		on, off = b, a
	}
	if recallTotal(on) == 0 {
		return ""
	}
	offByID := make(map[string]result, len(off))
	for _, r := range off {
		offByID[r.ID] = r
	}
	paired, onPass, offPass := 0, 0, 0
	var helpful, harmful []string
	overheadChars := 0
	for _, r := range on {
		counterpart, ok := offByID[r.ID]
		if !ok || r.Skipped || counterpart.Skipped {
			continue
		}
		paired++
		overheadChars += r.MemoryRecallChars
		if r.Passed {
			onPass++
		}
		if counterpart.Passed {
			offPass++
		}
		switch {
		case r.Passed && !counterpart.Passed:
			helpful = append(helpful, r.ID)
		case !r.Passed && counterpart.Passed && r.MemoryRecallEvents > 0:
			harmful = append(harmful, r.ID)
		}
	}
	if paired == 0 {
		return ""
	}
	var s strings.Builder
	s.WriteString("\n## Memory utility (paired counterfactual)\n\n")
	fmt.Fprintf(&s, "**Utility delta** %+.1fpp (on %d/%d vs off %d/%d, %d paired tasks) · ",
		100*(float64(onPass)-float64(offPass))/float64(paired), onPass, paired, offPass, paired, paired)
	fmt.Fprintf(&s, "**helpful** %d · **harmful** %d · **avg injected chars** %d\n", len(helpful), len(harmful), overheadChars/paired)
	if len(helpful) > 0 {
		fmt.Fprintf(&s, "\n- helpful (on-pass, off-fail): %s\n", strings.Join(helpful, ", "))
	}
	if len(harmful) > 0 {
		fmt.Fprintf(&s, "- harmful (on-fail, off-pass, recall fired): %s\n", strings.Join(harmful, ", "))
	}
	s.WriteString("\n<sub>Harmful attribution is paired, not judged: the same task passed without memory and failed with it while recall fired. Point-of-use markers live in the per-arm Memory shadow line.</sub>\n")
	return s.String()
}

func loadResults(path string) ([]result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var results []result
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func recallTotal(results []result) int {
	total := 0
	for _, r := range results {
		total += r.MemoryRecallEvents
	}
	return total
}
