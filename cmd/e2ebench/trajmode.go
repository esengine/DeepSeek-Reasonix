package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// runTrajMode re-digests already-recorded trajectory files, so digest changes
// can be evaluated against past runs without re-spending provider tokens.
// Wall-clock startup is unknown offline, so the decomposition omits it.
func runTrajMode(dir string) (string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.trajectory.jsonl"))
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no *.trajectory.jsonl files under %s", dir)
	}
	sort.Strings(paths)

	var b strings.Builder
	var results []result
	for _, p := range paths {
		s, err := summarizeTrajectory(p)
		if err != nil {
			return "", fmt.Errorf("%s: %w", p, err)
		}
		id := strings.TrimSuffix(filepath.Base(p), ".trajectory.jsonl")
		results = append(results, result{task: task{ID: id}, Trajectory: s})
		enc, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "### `%s`\n\n```json\n%s\n```\n\n", id, enc)
	}
	return "## Trajectory digest\n\n" + renderTimeAttribution(results) + renderToolSurface(results) + renderMechanismLedger(results) + b.String(), nil
}

func emitTrajMode(dir, outMD string) {
	report, err := runTrajMode(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "traj mode:", err)
		os.Exit(1)
	}
	emit(report, outMD, "")
}
