package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// trajectorySummary is the harness-side digest of one run's trajectory file:
// where the wall clock went, split between tool execution and everything the
// model spent between calls (thinking, streaming, provider latency).
type trajectorySummary struct {
	Path    string `json:"path"`
	Records int    `json:"records"`
	SpanMs  int64  `json:"span_ms"`
	ToolMs  int64  `json:"tool_ms"`
	ModelMs int64  `json:"model_ms"` // SpanMs − ToolMs, floored at zero
}

// trajectoryRecord is the subset of trajectory.Record the summary needs.
type trajectoryRecord struct {
	TS    int64 `json:"ts"`
	Event *struct {
		Kind string `json:"kind"`
		Tool *struct {
			DurationMs int64  `json:"durationMs"`
			ParentID   string `json:"parentId"`
		} `json:"tool"`
	} `json:"event"`
}

// renderTimeAttribution aggregates recorded runs into one report line; empty
// when no run in the suite carried a trajectory.
func renderTimeAttribution(results []result) string {
	var toolMs, modelMs int64
	runs := 0
	for _, r := range results {
		if r.Trajectory != nil {
			runs++
			toolMs += r.Trajectory.ToolMs
			modelMs += r.Trajectory.ModelMs
		}
	}
	if runs == 0 {
		return ""
	}
	return fmt.Sprintf("**Time attribution** (%d recorded runs): **tools** %s (%s) · **model** %s (%s)\n\n",
		runs, dur(toolMs), pct(int(toolMs), int(toolMs+modelMs)),
		dur(modelMs), pct(int(modelMs), int(toolMs+modelMs)))
}

// summarizeTrajectory reads a run's JSONL trajectory. A truncated final line
// (killed run) is skipped, matching the recorder's durability contract.
func summarizeTrajectory(path string) (*trajectorySummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := &trajectorySummary{Path: path}
	var firstTS, lastTS int64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		var rec trajectoryRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue
		}
		s.Records++
		if firstTS == 0 {
			firstTS = rec.TS
		}
		lastTS = rec.TS
		if rec.Event == nil || rec.Event.Tool == nil {
			continue
		}
		// Subagent calls overlap the parent's wall clock; counting them would
		// double-book the span.
		if rec.Event.Kind == "tool_result" && rec.Event.Tool.ParentID == "" {
			s.ToolMs += rec.Event.Tool.DurationMs
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	s.SpanMs = lastTS - firstTS
	if s.ModelMs = s.SpanMs - s.ToolMs; s.ModelMs < 0 {
		s.ModelMs = 0
	}
	return s, nil
}
