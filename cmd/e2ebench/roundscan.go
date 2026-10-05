package main

import (
	"bufio"
	"encoding/json"
	"os"
)

// boundarySplit is everything countable on each side of the first-correct
// instant — the numbers that end the exploration-vs-termination argument.
type boundarySplit struct {
	RoundsBefore, RoundsAfter    int
	CallsBefore, CallsAfter      int
	VerifyAfter                  int
	ReviewsAfter, MutationsAfter int
}

// splitAtCorrect scans a trajectory once and tallies rounds, tool calls,
// verifications, reviews and mutations relative to the cutoff instant.
func splitAtCorrect(path string, cutoffUnixMs int64) boundarySplit {
	var out boundarySplit
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	inModel := true
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		var rec trajectoryRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue
		}
		if rec.Event == nil || rec.Event.Tool == nil || rec.Event.Tool.ParentID != "" {
			continue
		}
		after := rec.TS > cutoffUnixMs
		switch rec.Event.Kind {
		case "tool_dispatch":
			if inModel {
				inModel = false
				if after {
					out.RoundsAfter++
				} else {
					out.RoundsBefore++
				}
			}
		case "tool_result":
			inModel = true
			tl := rec.Event.Tool
			if after {
				out.CallsAfter++
			} else {
				out.CallsBefore++
			}
			if v := tl.Execution; v != nil && after && (v.Verification == "passed" || v.Verification == "failed") {
				out.VerifyAfter++
			}
			if after && tl.Name == "review_report" {
				out.ReviewsAfter++
			}
			if after && !tl.ReadOnly && !bookkeepingTools[tl.Name] && tl.Name != "review_report" {
				out.MutationsAfter++
			}
		}
	}
	return out
}

// roundEnds returns the unix-ms end of each top-level tool round: the last
// tool_result before the next round's dispatch. The final answer segment has
// no entry — its end state is the run's final grade.
func roundEnds(path string) []int64 {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var ends []int64
	var lastResult int64
	inModel := true
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		var rec trajectoryRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue
		}
		if rec.Event == nil || rec.Event.Tool == nil || rec.Event.Tool.ParentID != "" {
			continue
		}
		switch rec.Event.Kind {
		case "tool_dispatch":
			if inModel && lastResult > 0 {
				ends = append(ends, lastResult)
			}
			inModel = false
		case "tool_result":
			inModel = true
			lastResult = rec.TS
		}
	}
	if lastResult > 0 {
		ends = append(ends, lastResult)
	}
	return ends
}
