package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	slices.Sort(paths)

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
	return "## Trajectory digest\n\n" + renderTimeAttribution(results) + renderToolSurface(results) + renderRefusals(results) + renderOutcomeProgress(results) + renderCognition(results) + renderMechanismLedger(results) + b.String(), nil
}

func emitTrajMode(dir, outMD string) {
	report, err := runTrajMode(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "traj mode:", err)
		os.Exit(1)
	}
	emit(report, outMD, "")
}

// offlineModeArgs is what a mode that reads recorded runs needs; every one of
// them re-digests what a suite run already wrote instead of spending tokens.
type offlineModeArgs struct {
	trajDir  string
	suite    string
	reportIn string
	outMD    string
	outJSON  string
	addr     string
}

// dispatchOfflineMode runs mode if it is one of the offline ones and reports
// whether it handled it, so main keeps flag parsing and nothing else.
func dispatchOfflineMode(mode string, a offlineModeArgs) bool {
	fail := func(name string, err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, name+" mode:", err)
			os.Exit(1)
		}
	}
	switch mode {
	case "compare":
		runCompareMode(a.outMD)
	case "rollout":
		fail("rollout", runRolloutMode(a.outMD, a.outJSON))
	case "traj":
		emitTrajMode(a.trajDir, a.outMD)
	case "barrier":
		report, err := runBarrierMode(a.trajDir)
		fail("barrier", err)
		emit(report, a.outMD, "")
	case "sft":
		fail("sft", runSFTMode(a.trajDir, a.suite, a.reportIn, a.outMD))
	case "serve":
		fail("serve", runServeMode(a.trajDir, a.suite, a.addr))
	default:
		return false
	}
	return true
}

// absTrajectoryDir anchors -trajectories to the operator's cwd. Each task's
// child runs with the task workdir as its cwd, so a relative value would name
// a directory under every workdir instead of the one the operator meant.
func absTrajectoryDir(dir string) string {
	if dir == "" {
		return ""
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}
