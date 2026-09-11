package main

import "fmt"

// roundDigest is one model round's cost/outcome line: the gap that preceded
// the batch, the executor thinking that gap bought, and what the round did.
type roundDigest struct {
	Index            int      `json:"i"`
	Outcome          string   `json:"outcome"`
	GapMs            int64    `json:"gap_ms"`
	ToolMs           int64    `json:"tool_ms,omitempty"`
	ReasoningTokens  int64    `json:"reasoning_tokens,omitempty"`
	CompletionTokens int64    `json:"completion_tokens,omitempty"`
	PromptTokens     int64    `json:"prompt_tokens,omitempty"`
	Actions          []string `json:"actions,omitempty"`
}

// slowRoundGapMs is the census threshold: a model gap this long is a large
// cognition purchase worth itemizing (p90-tail rounds sit well above it).
const slowRoundGapMs = 8000

// recordRound books one classified round into both the outcome tallies and
// the per-round cognition digest. A nil batch is a finalization round.
func (t *trajScan) recordRound(outcome string, gap gapInfo, b *toolBatch) {
	t.recordOutcome(outcome, gap.ms)
	d := roundDigest{
		Index: len(t.s.Rounds) + 1, Outcome: outcome, GapMs: gap.ms,
		ReasoningTokens: gap.reasonTok, CompletionTokens: gap.complTok,
		PromptTokens: gap.promptTok,
	}
	if b != nil {
		d.ToolMs = b.serialMs
		d.Actions = append([]string(nil), b.names...)
	}
	t.s.Rounds = append(t.s.Rounds, d)
	// Recovery- and compaction-tainted gaps are provider/host time, not a
	// cognition purchase; letting them into the census would misattribute a
	// 30s retry as a slow thinking round.
	if gap.ms >= slowRoundGapMs && !gap.tainted && !gap.compaction {
		t.s.SlowRounds++
		t.s.SlowRoundGapMs += gap.ms
		t.s.SlowRoundReasoningTokens += gap.reasonTok
	}
}

// renderDelegationAdmission aggregates the shadow admission verdicts: how many
// expensive delegation calls a local-fix boundary would have refused, and the
// subagent time those refusals would have reclaimed.
func renderDelegationAdmission(results []result) string {
	calls, denies := 0, 0
	var deniedMs int64
	for _, r := range results {
		if r.Trajectory == nil {
			continue
		}
		calls += r.Trajectory.DelegationCalls
		denies += r.Trajectory.DelegationDenies
		deniedMs += r.Trajectory.DeniedDelegationMs
	}
	if calls == 0 {
		return ""
	}
	line := fmt.Sprintf("**Delegation admission** (shadow): **%d** gated calls · **would deny** %d (%s)",
		calls, denies, pct(denies, calls))
	if deniedMs > 0 {
		line += fmt.Sprintf(" · **subagent time behind denied calls** %s", dur(deniedMs))
	}
	return line + "\n\n"
}

// renderCognition prices what the model's thinking bought: totals, the output
// rate (uniform rates indict token volume, not serving), the slow-round
// census, and delegation cost. Empty when no run carried usage-joined rounds.
func renderCognition(results []result) string {
	var reason, compl, slowGapMs, gapMs, delegToolMs int64
	slow, delegRounds, runs, solved := 0, 0, 0, 0
	var slowReason int64
	var rates []int64
	for _, r := range results {
		if r.Passed {
			solved++
		}
		t := r.Trajectory
		if t == nil || len(t.Rounds) == 0 {
			continue
		}
		runs++
		reason += t.ReasoningTokensTotal
		compl += t.CompletionTokensTotal
		slow += t.SlowRounds
		slowGapMs += t.SlowRoundGapMs
		slowReason += t.SlowRoundReasoningTokens
		gapMs += t.ModelGapTotalMs
		for _, d := range t.Rounds {
			if d.Outcome == "delegation" {
				delegRounds++
				delegToolMs += d.ToolMs
			}
			if d.GapMs >= 1000 && d.ReasoningTokens+d.CompletionTokens > 0 {
				rates = append(rates, (d.ReasoningTokens+d.CompletionTokens)*1000/d.GapMs)
			}
		}
	}
	if runs == 0 {
		return ""
	}
	line := fmt.Sprintf("**Cognition** (%d recorded runs): **reasoning** %s tok · **completion** %s tok",
		runs, comma(int(reason)), comma(int(compl)))
	if solved > 0 {
		line += fmt.Sprintf(" (**%s reasoning/solved**)", comma(int(reason/int64(solved))))
	}
	if len(rates) > 0 {
		line += fmt.Sprintf(" · **output rate** p50 %d · p90 %d tok/s", pctile(rates, 50), pctile(rates, 90))
	}
	if slow > 0 {
		line += fmt.Sprintf(" · **slow rounds** (≥%ds) %d = %s of model time, %s reasoning tok",
			slowRoundGapMs/1000, slow, pct(int(slowGapMs), int(gapMs)), comma(int(slowReason)))
	}
	if delegRounds > 0 {
		line += fmt.Sprintf(" · **delegation** %d rounds (%s in subagents)", delegRounds, dur(delegToolMs))
	}
	return line + "\n\n"
}
