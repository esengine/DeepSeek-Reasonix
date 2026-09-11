package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// runSegments drives a task's legs and folds their accounting into one result.
// Each leg writes its own metrics file: sharing one path would let the last
// leg's numbers stand in for the whole run, and the tokens the earlier legs
// spent would simply vanish. Only the final leg's trajectory digest is kept;
// Segments records how many legs it does not cover.
func runSegments(ctx context.Context, cfg suiteConfig, t task, work, trajDir string, env []string, r *result) error {
	segs := planSegments(t, cfg.segments, cfg.steers)
	r.Segments = len(segs)
	var runErr error
	for _, seg := range segs {
		metricsPath := filepath.Join(work, fmt.Sprintf(".run-metrics-%d.json", seg.index))
		trajPath := segmentTrajectoryPath(trajDir, t.ID, seg, len(segs))
		args := buildSegmentArgs(cfg, seg, metricsPath, trajPath)

		cmd := exec.CommandContext(ctx, cfg.bin, args...)
		cmd.Dir = work
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), env...)
		}
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		cmd.WaitDelay = 10 * time.Second
		runErr = cmd.Run()

		if m, err := readMetrics(metricsPath); err == nil {
			foldSegmentMetrics(r, m, seg.index)
		}
		// A leg that died takes the run with it: resuming a session the child
		// never finished writing would measure the harness's crash recovery,
		// which is a different experiment.
		if runErr != nil || ctx.Err() != nil {
			break
		}
	}
	return runErr
}

// segmentTrajectoryPath keeps one file per leg so a resumed leg cannot truncate
// the record of the one before it.
func segmentTrajectoryPath(dir, id string, seg segment, total int) string {
	if dir == "" {
		return ""
	}
	if total < 2 {
		return filepath.Join(dir, id+".trajectory.jsonl")
	}
	return filepath.Join(dir, fmt.Sprintf("%s.seg%d.trajectory.jsonl", id, seg.index))
}

// lastSegmentTrajectory names the file whose digest represents the run. Only
// the final leg's is read; Segments in the JSON says how many were not.
func lastSegmentTrajectory(dir, id string, segments int) string {
	if dir == "" {
		return ""
	}
	if segments < 2 {
		return filepath.Join(dir, id+".trajectory.jsonl")
	}
	return filepath.Join(dir, fmt.Sprintf("%s.seg%d.trajectory.jsonl", id, segments))
}

// foldSegmentMetrics adds one leg's spend to the run. The first leg's metrics
// establish the non-additive fields (currency, outcome); later legs contribute
// their totals, and the last leg's outcome wins because it is the one that
// ended the run.
func foldSegmentMetrics(r *result, m runMetrics, index int) {
	if index == 1 {
		r.runMetrics = m
		return
	}
	r.PromptTokens += m.PromptTokens
	r.CompletionTokens += m.CompletionTokens
	r.CacheHitTokens += m.CacheHitTokens
	r.CacheMissTokens += m.CacheMissTokens
	r.Cost += m.Cost
	r.Steps += m.Steps
	r.Compactions += m.Compactions
	r.ToolCalls += m.ToolCalls
	r.ToolFailures += m.ToolFailures
	r.SubagentToolCalls += m.SubagentToolCalls
	r.Retries += m.Retries
	r.Complete = m.Complete
	if m.Outcome != "" {
		r.Outcome = m.Outcome
	}
	for name, n := range m.ToolCallsByName {
		if r.ToolCallsByName == nil {
			r.ToolCallsByName = map[string]int{}
		}
		r.ToolCallsByName[name] += n
	}
	for reason, n := range m.PrefixChangeReasonCounts {
		if r.PrefixChangeReasonCounts == nil {
			r.PrefixChangeReasonCounts = map[string]int{}
		}
		r.PrefixChangeReasonCounts[reason] += n
	}
	accumulateSources(r.UsageBySource, m.UsageBySource)
}
