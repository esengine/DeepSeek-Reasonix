package main

import (
	"fmt"
	"strings"
)

// mechanismRow aggregates one extra-round mechanism across a suite: how often
// it fired, what its rounds cost, and how runs where it fired graded versus
// runs where it stayed quiet. Correlation, not causation — the causal rescue
// rate needs an ablation arm A/B (-ablate + -mode compare).
type mechanismRow struct {
	fires                  int
	ms                     int64
	msKnown                bool
	firedRuns, firedSolved int
	quietRuns, quietSolved int
}

// mechanismOrder fixes the ledger's row order: correctness nudges first, then
// provider recovery, then structural overhead.
var mechanismOrder = []string{
	"handoff_nudge", "empty_final_retry", "no_progress_signal",
	"stream_retry", "header_retry", "reasoning_replay",
	"planner", "compaction", "bookkeeping", "duplicate_work",
	"subagent", "capability_router", "goal_evaluator", "tool_source_connect", "prefix_reset",
}

// mechanismFacts extracts one run's (fires, attributed ms, ms known) per
// mechanism from its digest and metrics.
func mechanismFacts(r result) map[string]mechanismRow {
	t := r.Trajectory
	if t == nil {
		return nil
	}
	byKind := func(kind string) int64 { return t.RecoveryGapMsByKind[kind] }
	facts := map[string]mechanismRow{
		"handoff_nudge":       {fires: t.HandoffNudges, ms: t.RoundOutcomeMs["handoff_retry"], msKnown: true},
		"empty_final_retry":   {fires: t.EmptyFinalRetries, ms: byKind("empty_final_retry"), msKnown: true},
		"no_progress_signal":  {fires: t.NoProgressSignals, msKnown: false},
		"stream_retry":        {fires: t.StreamRetries, ms: byKind("stream_retry"), msKnown: true},
		"header_retry":        {fires: t.HeaderRetries, ms: byKind("header_retry"), msKnown: true},
		"reasoning_replay":    {fires: t.ReasoningReplays, ms: byKind("reasoning_replay"), msKnown: true},
		"planner":             {fires: t.PlannerRequests, ms: t.RoundOutcomeMs["planning"], msKnown: true},
		"compaction":          {fires: t.Compactions, ms: t.RoundOutcomeMs["compaction"], msKnown: true},
		"bookkeeping":         {fires: t.RoundOutcomes["bookkeeping"], ms: t.RoundOutcomeMs["bookkeeping"], msKnown: true},
		"duplicate_work":      {fires: t.RoundOutcomes["duplicate_work"], ms: t.RoundOutcomeMs["duplicate_work"], msKnown: true},
		"subagent":            {fires: t.SubagentRequests, msKnown: false},
		"capability_router":   {fires: r.CapabilityRoutes, ms: r.CapabilityRouterLatencyMs, msKnown: true},
		"tool_source_connect": {fires: t.ConnectCalls, msKnown: false},
		"prefix_reset":        {fires: t.PrefixResets, msKnown: false},
		"goal_evaluator":      {fires: t.RequestsBySource["goal-evaluator"], msKnown: false},
	}
	return facts
}

// renderToolSurface is the schema-tax line: what every request re-pays for
// the visible tool surface, and the churn (connects, prefix resets) the
// economy tier trades that tax against. Fresh-session benchmarks re-pay the
// miss on every task, so the surface size prices differently than in a
// long-lived session.
func renderToolSurface(results []result) string {
	var schemaMax, schemaTotal, promptTotal int64
	connects, resets, runs := 0, 0, 0
	for _, r := range results {
		t := r.Trajectory
		if t == nil || t.SchemaTokensTotal == 0 {
			continue
		}
		runs++
		schemaMax = max(schemaMax, t.SchemaTokensMax)
		schemaTotal += t.SchemaTokensTotal
		promptTotal += t.PromptTokensSeen
		connects += t.ConnectCalls
		resets += t.PrefixResets
	}
	if runs == 0 {
		return ""
	}
	line := fmt.Sprintf("**Tool surface**: **schema footprint** %s tok/request (max) · **Σ schema tax** %s tok", comma(int(schemaMax)), comma(int(schemaTotal)))
	if promptTotal > 0 {
		line += fmt.Sprintf(" (%s of prompt)", pct(int(schemaTotal), int(promptTotal)))
	}
	line += fmt.Sprintf(" · **connect_tool_source** ×%d · **prefix resets** %d\n\n", connects, resets)
	return line
}

// renderMechanismLedger is the measure-before-cutting table: per mechanism,
// incidence, attributed model time, and solved rates fired-vs-quiet. All-quiet
// suites render a single line so absence is a stated result, not a blank.
func renderMechanismLedger(results []result) string {
	rows := map[string]mechanismRow{}
	recorded := 0
	for _, r := range results {
		facts := mechanismFacts(r)
		if facts == nil {
			continue
		}
		recorded++
		for name, f := range facts {
			row := rows[name]
			row.fires += f.fires
			row.ms += f.ms
			row.msKnown = row.msKnown || f.msKnown
			if f.fires > 0 {
				row.firedRuns++
				if r.Passed {
					row.firedSolved++
				}
			} else {
				row.quietRuns++
				if r.Passed {
					row.quietSolved++
				}
			}
			rows[name] = row
		}
	}
	if recorded == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("**Mechanism ledger** (incidence → cost → outcome; correlation only — causal rescue rates need an `-ablate` A/B):\n\n")
	fired := 0
	b.WriteString("| Mechanism | Fires | Runs fired | Time | Solved (fired) | Solved (quiet) |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|\n")
	for _, name := range mechanismOrder {
		row := rows[name]
		if row.fires == 0 {
			continue
		}
		fired++
		ms := "—"
		if row.msKnown {
			ms = dur(row.ms)
		}
		fmt.Fprintf(&b, "| %s | %d | %d/%d | %s | %s | %s |\n",
			name, row.fires, row.firedRuns, recorded, ms,
			pct(row.firedSolved, row.firedRuns), pct(row.quietSolved, row.quietRuns))
	}
	if fired == 0 {
		return fmt.Sprintf("**Mechanism ledger**: all quiet — no extra-round machinery fired across %d recorded runs.\n\n", recorded)
	}
	return b.String() + "\n"
}

// renderContractShadow prices the shadow contract against the hidden grader:
// agreement is the number the adoption decision is made on. Absent audits
// (agent without shadow wiring) render nothing.
func renderContractShadow(results []result) string {
	agree, disagree := 0, 0
	verdicts := map[string]int{}
	for _, r := range results {
		t := r.Trajectory
		if t == nil || t.ShadowVerdict == "" {
			continue
		}
		verdicts[t.ShadowVerdict]++
		if t.ShadowComplete == r.Passed {
			agree++
		} else {
			disagree++
		}
	}
	if agree+disagree == 0 {
		return ""
	}
	parts := make([]string, 0, len(verdicts))
	for _, v := range []string{"complete", "continue", "blocked", "uncertain"} {
		if verdicts[v] > 0 {
			parts = append(parts, fmt.Sprintf("%s ×%d", v, verdicts[v]))
		}
	}
	return fmt.Sprintf("**Contract shadow**: verdicts %s · **agreement with grader** %s (%d/%d)\n\n",
		strings.Join(parts, " · "), pct(agree, agree+disagree), agree, agree+disagree)
}
