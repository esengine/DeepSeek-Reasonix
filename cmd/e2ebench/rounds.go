package main

import (
	"fmt"
	"slices"
	"strings"
)

// renderRoundEfficiency is the knife-target line: how many rounds bought
// progress, where the wasted model seconds went, and what a solve pays for
// the waste (failed runs' waste charged to the solves, like every per-solved
// figure).
func renderRoundEfficiency(results []result) string {
	useful, classified, solved := 0, 0, 0
	var wastedMs int64
	wasteCount := map[string]int{}
	wasteMs := map[string]int64{}
	for _, r := range results {
		if r.Passed {
			solved++
		}
		if r.Trajectory == nil {
			continue
		}
		useful += r.Trajectory.UsefulRounds
		wastedMs += r.Trajectory.WastedGapMs
		for outcome, n := range r.Trajectory.RoundOutcomes {
			classified += n
			if !productiveOutcomes[outcome] {
				wasteCount[outcome] += n
				wasteMs[outcome] += r.Trajectory.RoundOutcomeMs[outcome]
			}
		}
	}
	if classified == 0 {
		return ""
	}
	line := fmt.Sprintf("\n\n**Round efficiency**: **useful rounds** %d/%d (%s) · **wasted model time** %s",
		useful, classified, pct(useful, classified), dur(wastedMs))
	if solved > 0 {
		line += fmt.Sprintf(" (**%s/solved**)", dur(wastedMs/int64(solved)))
	}
	outcomes := make([]string, 0, len(wasteMs))
	for outcome := range wasteMs {
		outcomes = append(outcomes, outcome)
	}
	slices.SortFunc(outcomes, func(a, b string) int {
		if wasteMs[a] != wasteMs[b] {
			return int(wasteMs[b] - wasteMs[a])
		}
		return strings.Compare(a, b)
	})
	parts := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		parts = append(parts, fmt.Sprintf("%s ×%d (%s)", outcome, wasteCount[outcome], dur(wasteMs[outcome])))
	}
	if len(parts) > 0 {
		line += " · **waste breakdown**: " + strings.Join(parts, " · ")
	}
	return line
}

// trajScan is the running state of one trajectory pass.
type trajScan struct {
	s                  *trajectorySummary
	firstTS, lastTS    int64
	orphanMs, gapStart int64
	gaps, cleanGaps    []int64
	delays             []int64
	allIntervals       [][2]int64
	inModel            bool
	taint              string
	streakRun          int
	batch              *toolBatch

	attemptBegin            map[string]int64
	attempts                []modelAttempt
	lastAttempt             int // most recent closed attempt awaiting a usage tag
	pendingRetry, compFrom  int64
	retryIvs, compIvs       [][2]int64
	firstDelta, firstToolTS int64

	pendingGaps                        []gapInfo
	seen                               map[string]bool // (name, args) pairs already dispatched
	gapPlanner, gapCompact, gapHandoff bool
	sawCallIDs                         bool

	outcomePoints          []outcomePoint
	verifySeen, verifyPass map[string]bool
	verifyPoints           []verifyPoint

	gapReason, gapCompl, gapPrompt int64

	denyDelegations  map[string]bool
	delegationToolMs map[string]int64
}

// modelAttempt is one sampling attempt's wall interval; planner marks attempts
// whose closing usage event carried source "planner".
type modelAttempt struct {
	iv      [2]int64
	planner bool
}

// productiveOutcomes are rounds that moved the task forward; everything else
// is the wasted/questionable bucket the report itemizes.
var productiveOutcomes = map[string]bool{
	"evidence_gain": true, "mutation": true, "verification": true, "finalization": true,
	"delegation": true,
}

// delegationTools are calls whose cost story is the delegation itself, not the
// local mutation/verification the batch would otherwise classify as.
var delegationTools = map[string]bool{
	"task": true, "parallel_tasks": true, "fleet": true, "research": true,
}

// bookkeepingTools are ledger tools whose rounds cost a full round-trip
// without touching the workspace — bookkeeping cost remains visible even
// though ordered complete_step sign-offs may now share a provider round.
var bookkeepingTools = map[string]bool{
	"complete_step": true, "todo_write": true, "wait": true, "bash_output": true,
}

// classifyRound names what one round's gap bought. Gap-level signals outrank
// batch analysis; a nil batch is the final answer round. Repeated failures
// land in duplicate_work; a first failure still counts as evidence (it
// localizes), matching the progress guard's scoring.
func classifyRound(gap gapInfo, b *toolBatch) string {
	switch {
	case gap.tainted:
		return "recovery"
	case gap.compaction:
		return "compaction"
	case gap.planner:
		return "planning"
	case gap.handoff:
		return "handoff_retry"
	}
	if b == nil {
		return "finalization"
	}
	verification, mutation, delegation := false, false, false
	allBookkeeping, allDup := true, true
	for _, c := range b.infos {
		if c.verification == "passed" || c.verification == "failed" {
			verification = true
		}
		if delegationTools[c.name] {
			delegation = true
		}
		if c.resolved && !c.readOnly && !c.errored && !bookkeepingTools[c.name] {
			mutation = true
		}
		if !bookkeepingTools[c.name] {
			allBookkeeping = false
		}
		if !c.dup {
			allDup = false
		}
	}
	switch {
	case delegation:
		return "delegation"
	case verification:
		return "verification"
	case mutation:
		return "mutation"
	case allBookkeeping:
		return "bookkeeping"
	case allDup:
		return "duplicate_work"
	}
	return "evidence_gain"
}

func (t *trajScan) recordOutcome(outcome string, ms int64) {
	s := t.s
	if s.RoundOutcomes == nil {
		s.RoundOutcomes = map[string]int{}
		s.RoundOutcomeMs = map[string]int64{}
	}
	s.RoundOutcomes[outcome]++
	s.RoundOutcomeMs[outcome] += ms
	if productiveOutcomes[outcome] {
		s.UsefulRounds++
		return
	}
	s.WastedGapMs += ms
}
