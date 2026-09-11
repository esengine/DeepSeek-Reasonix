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

	"reasonix/internal/config"
)

// seedTaskMemory populates an isolated memory state root from the task's
// memory/ directory (memory/project/*.md and memory/global/*.md) and returns
// the env entry pointing the child at it. Tasks without seeds get no env, so
// ordinary suites keep using the developer's real store root untouched.
func seedTaskMemory(taskDir, work string) ([]string, error) {
	seeds := filepath.Join(taskDir, "memory")
	if _, err := os.Stat(seeds); err != nil {
		return nil, nil
	}
	stateHome, err := os.MkdirTemp("", "e2ebench-mem-")
	if err != nil {
		return nil, err
	}
	absWork, err := filepath.Abs(work)
	if err != nil {
		return nil, err
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
			return nil, err
		}
		if err := copyDir(pair[0], pair[1]); err != nil {
			return nil, err
		}
	}
	return []string{"REASONIX_STATE_HOME=" + stateHome}, nil
}

// taskExperimentEnv assembles one run's experiment environment: the policy
// arm, fork capture, and the seeded memory state root. The note reports a
// seeding failure without aborting the run.
func taskExperimentEnv(cfg suiteConfig, t task, work string) (env []string, note string) {
	switch cfg.policy {
	case "ebm":
		env = append(env, "REASONIX_EXPERIMENT_EBM=1")
	case "governor":
		env = append(env, "REASONIX_EXPERIMENT_GOVERNOR=1")
	case "memory-off":
		env = append(env, "REASONIX_EXPERIMENT_NO_MEMORY=1")
	}
	if cfg.forkCapture != "" {
		env = append(env, "REASONIX_EXPERIMENT_FORK_CAPTURE_DIR="+filepath.Join(cfg.forkCapture, t.ID))
	}
	seedEnv, err := seedTaskMemory(t.dir, work)
	if err != nil {
		return env, "memory seed: " + err.Error()
	}
	return append(env, seedEnv...), ""
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
