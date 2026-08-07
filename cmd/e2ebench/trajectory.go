package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"slices"
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
	// Round decomposition: a round's gap runs from the turn start (or the
	// batch's last tool_result) to the next top-level tool_dispatch; the
	// final segment to the last record is the answer round.
	ModelRounds     int   `json:"model_rounds"`
	ModelGapTotalMs int64 `json:"model_gap_total_ms"`
	ModelGapP95Ms   int64 `json:"model_gap_p95_ms"`
	Retries         int   `json:"retries,omitempty"`
	Compactions     int   `json:"compactions,omitempty"`
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
	var toolMs, modelMs, gapMs int64
	runs, rounds := 0, 0
	for _, r := range results {
		if r.Trajectory != nil {
			runs++
			toolMs += r.Trajectory.ToolMs
			modelMs += r.Trajectory.ModelMs
			rounds += r.Trajectory.ModelRounds
			gapMs += r.Trajectory.ModelGapTotalMs
		}
	}
	if runs == 0 {
		return ""
	}
	line := fmt.Sprintf("**Time attribution** (%d recorded runs): **tools** %s (%s) · **model** %s (%s)",
		runs, dur(toolMs), pct(int(toolMs), int(toolMs+modelMs)),
		dur(modelMs), pct(int(modelMs), int(toolMs+modelMs)))
	if rounds > 0 {
		line += fmt.Sprintf(" · **model rounds** %d (avg gap %s)", rounds, dur(gapMs/int64(rounds)))
	}
	return line + "\n\n"
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
	var gaps []int64
	var gapStart int64
	inModel := false
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
			gapStart = rec.TS
			inModel = true
		}
		lastTS = rec.TS
		if rec.Event == nil {
			continue
		}
		switch rec.Event.Kind {
		case "retrying":
			s.Retries++
		case "compaction_started":
			s.Compactions++
		}
		if rec.Event.Tool == nil || rec.Event.Tool.ParentID != "" {
			// Subagent calls overlap the parent's wall clock; counting them
			// would double-book the span and split the parent's rounds.
			continue
		}
		switch rec.Event.Kind {
		case "tool_dispatch":
			if inModel {
				gaps = append(gaps, rec.TS-gapStart)
				inModel = false
			}
		case "tool_result":
			s.ToolMs += rec.Event.Tool.DurationMs
			gapStart = rec.TS
			inModel = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if inModel && lastTS > gapStart {
		gaps = append(gaps, lastTS-gapStart) // final answer round
	}
	s.ModelRounds = len(gaps)
	for _, g := range gaps {
		s.ModelGapTotalMs += g
	}
	s.ModelGapP95Ms = p95(gaps)
	s.SpanMs = lastTS - firstTS
	if s.ModelMs = s.SpanMs - s.ToolMs; s.ModelMs < 0 {
		s.ModelMs = 0
	}
	return s, nil
}

func p95(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	slices.Sort(sorted)
	index := min((len(sorted)*95+99)/100, len(sorted))
	return sorted[index-1]
}
