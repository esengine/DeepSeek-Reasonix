package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

func render(results []result) string {
	arm := "full"
	if len(results) > 0 {
		if results[0].Arm != "" {
			arm = results[0].Arm
		}
	}
	cache := ""
	if len(results) > 0 && results[0].CacheArm != "" && results[0].CacheArm != benchmarkCacheCold {
		cache = " · " + results[0].CacheArm + "-cache"
	}
	return fmt.Sprintf("## 🤖 Reasonix e2e benchmark (arm `%s`%s)\n\n", arm, cache) + renderBody(results)
}

// suiteStats aggregates result entries; ran/pass1 count tasks (first
// attempts), everything else accumulates across every attempt.
type suiteStats struct {
	passed, ran, pass1, maxAttempt                                        int
	accounted, accountedSolved, unaccounted, unaccountedSolved, partial   int
	pTok, cTok, hit, miss, compacts, tools, toolFails, steps, modelRounds int
	cost                                                                  float64
	walls, ttcs, ttft, firstCorrect, postWaste                            []int64
	wallAccountedMs, wallTotalMs, firstHit, firstMiss                     int64
	solvedThenBroken, damaged, withCorrect                                int
	currency                                                              string
	classes, prefixChangeReasons                                          map[string]int
	bySource                                                              map[string]sourceUsage
}

func gatherSuiteStats(results []result) suiteStats {
	s := suiteStats{maxAttempt: 1, classes: map[string]int{}, prefixChangeReasons: map[string]int{}, bySource: map[string]sourceUsage{}}
	for _, r := range results {
		// No-solution tasks are graded on honesty, not correctness; leaving
		// them out here keeps every accuracy and cost-per-solved denominator
		// meaningful. renderCompletionIntegrity reports them, spend included.
		if r.Skipped || r.NoSolution {
			continue
		}
		// ran counts tasks, not attempts: retries add entries, first attempts
		// add denominators. Old JSON without Attempt keeps one entry per task.
		if r.Attempt <= 1 {
			s.ran++
			if r.Passed {
				s.pass1++
			}
		}
		s.maxAttempt = max(s.maxAttempt, r.Attempt)
		if r.Passed {
			s.passed++
			if r.TTCSMs > 0 {
				s.ttcs = append(s.ttcs, r.TTCSMs)
			} else {
				s.ttcs = append(s.ttcs, r.WallMs) // old JSON: single attempt
			}
		}
		s.wallTotalMs += r.WallMs
		s.classes[r.class()]++
		s.walls = append(s.walls, r.WallMs)
		if r.FirstCorrectMs > 0 {
			s.firstCorrect = append(s.firstCorrect, r.FirstCorrectMs)
			s.withCorrect++
			if r.RegressedAfterCorrect {
				s.damaged++
			}
			if r.Passed {
				s.postWaste = append(s.postWaste, r.PostSolveWasteMs)
			}
		}
		if r.SolvedThenBroken {
			s.solvedThenBroken++
		}
		if r.Unaccounted {
			s.unaccounted++
			if r.Passed {
				s.unaccountedSolved++
			}
			continue
		}
		s.accounted++
		if r.Passed {
			s.accountedSolved++
		}
		if r.Partial {
			s.partial++
		}
		s.pTok += r.PromptTokens
		s.cTok += r.CompletionTokens
		s.hit += r.CacheHitTokens
		s.miss += r.CacheMissTokens
		s.compacts += r.Compactions
		s.tools += r.ToolCalls
		s.toolFails += r.ToolFailures
		s.steps += r.Steps
		s.wallAccountedMs += r.WallMs
		accumulateSources(s.bySource, r.UsageBySource)
		if r.Trajectory != nil {
			s.modelRounds += r.Trajectory.ModelRounds
			if r.Trajectory.TTFTMs > 0 {
				s.ttft = append(s.ttft, r.Trajectory.TTFTMs)
			}
			s.firstHit += r.Trajectory.FirstReqCacheHitTokens
			s.firstMiss += r.Trajectory.FirstReqCacheMissTokens
		}
		s.cost += r.Cost
		if r.Currency != "" {
			s.currency = r.Currency
		}
		for reason, n := range r.PrefixChangeReasonCounts {
			s.prefixChangeReasons[reason] += n
		}
	}
	return s
}

// kpiLine centers the report on time-to-correct-solution: a fast wrong run is
// not fast. TTCS is measured over solved tasks only (a retried solve carries
// its failed attempts' wall), and solved/hour divides by every attempt's wall
// — failures cost real time whether or not a later attempt lands.
func kpiLine(s suiteStats) string {
	if s.ran == 0 {
		return ""
	}
	line := fmt.Sprintf("**KPI**: **Pass@1** %s", pct(s.pass1, s.ran))
	if s.maxAttempt > 1 {
		line += fmt.Sprintf(" · **Pass@≤%d** %s", s.maxAttempt, pct(s.passed, s.ran))
	}
	line += fmt.Sprintf(" · **TTCS median** %s · **TTCS p90** %s", dur(median(s.ttcs)), dur(pctile(s.ttcs, 90)))
	if s.wallTotalMs > 0 {
		line += fmt.Sprintf(" · **Solved/hour** %.1f", float64(s.passed)*3_600_000/float64(s.wallTotalMs))
	}
	if len(s.ttft) > 0 {
		line += fmt.Sprintf(" · **TTFT median** %s", durMs(median(s.ttft)))
	}
	if s.firstHit+s.firstMiss > 0 {
		line += fmt.Sprintf(" · **first-request cache hit** %s", pct(int(s.firstHit), int(s.firstHit+s.firstMiss)))
	}
	if len(s.firstCorrect) > 0 {
		line += fmt.Sprintf(" · **TTFCS median** %s · **post-solve waste median** %s", dur(median(s.firstCorrect)), dur(median(s.postWaste)))
		line += fmt.Sprintf(" · **overthinking damage** %s", pct(s.damaged, s.withCorrect))
		if s.solvedThenBroken > 0 {
			line += fmt.Sprintf(" · **solved-then-broke** %d", s.solvedThenBroken)
		}
	}
	return line + "\n\n"
}

// perSolvedLine is the efficiency-per-solve report line: total spend across
// every accounted run (failures included) divided by accounted solves, so a
// same-accuracy agent needing twice the rounds cannot hide behind averages.
func perSolvedLine(s suiteStats) string {
	if s.accountedSolved == 0 {
		return ""
	}
	line := fmt.Sprintf("**Per solved task:** **model requests** %.1f · tool calls %.1f · wall %s",
		float64(s.steps)/float64(s.accountedSolved), float64(s.tools)/float64(s.accountedSolved),
		dur(s.wallAccountedMs/int64(s.accountedSolved)))
	if s.modelRounds > 0 {
		line += fmt.Sprintf(" · model rounds %.1f", float64(s.modelRounds)/float64(s.accountedSolved))
	}
	return line + "\n\n"
}

// renderBody is the report without a heading, so a caller that supplies its own
// (SWE-bench mode) does not stack two titles.
func renderBody(results []result) string {
	var b strings.Builder
	s := gatherSuiteStats(results)

	// Cost and tokens are divided by the solved instances we actually have
	// accounting for. Dividing by every solve would treat a lost metrics file as
	// a free solve and understate the published figure.
	fmt.Fprintf(&b, "**Solved:** %d/%d (%s) · **Cost per solved:** %s · **Tokens per solved:** %s · **Median wall time:** %s\n\n",
		s.passed, s.ran, pct(s.passed, s.ran),
		costPerSolved(s.cost, s.accountedSolved, s.currency), tokensPerSolved(s.pTok+s.cTok, s.accountedSolved), dur(median(s.walls)))
	b.WriteString(kpiLine(s))
	fmt.Fprintf(&b, "**Cache hit:** %s · **Tokens:** %s (prompt %s / completion %s) · **Tool calls:** %s (%s failed) · **Compactions:** %d · **Cost:** %s%.4f\n\n",
		pct(s.hit, s.hit+s.miss), comma(s.pTok+s.cTok), comma(s.pTok), comma(s.cTok),
		comma(s.tools), comma(s.toolFails), s.compacts, currencySym(s.currency), s.cost)
	b.WriteString(perSolvedLine(s))
	b.WriteString(requestsBySourceLine(s.bySource))
	b.WriteString(renderMeterAccounting(results))
	b.WriteString(renderFaultRecovery(results))
	b.WriteString(renderTimeAttribution(results))
	b.WriteString(renderSolveProfiles(results))
	b.WriteString(renderToolSurface(results))
	b.WriteString(renderAnchorSafety(results))
	b.WriteString(renderContractShadow(results))
	b.WriteString(renderCompletionReport(results))
	b.WriteString(renderCompletionIntegrity(results))
	b.WriteString(renderOutcomeProgress(results))
	b.WriteString(renderMemoryShadow(results))
	b.WriteString(renderCognition(results))
	b.WriteString(renderAnchor(results))
	b.WriteString(renderDelegation(results))
	b.WriteString(renderDelegationAdmission(results))
	b.WriteString(renderMechanismLedger(results))
	if s.unaccounted > 0 {
		fmt.Fprintf(&b, "> **Accounting incomplete for %d of %d instances** (%d of them solved): the agent was killed before it wrote any metrics, so their cost and tokens are unknown. Totals above cover the %d accounted instances only, and per-solved figures divide by the %d accounted solves — the true totals are higher.\n\n",
			s.unaccounted, s.ran, s.unaccountedSolved, s.accounted, s.accountedSolved)
	}
	if s.partial > 0 {
		fmt.Fprintf(&b, "> **%d of %d instances contributed partial accounting**: the agent was killed mid-run and its numbers were recovered from the last in-flight snapshot. What is counted is real but stops at that snapshot, so every total above is a lower bound.\n\n",
			s.partial, s.ran)
	}

	renderTaskTable(&b, results)
	b.WriteString("\n" + renderTimelines(results))

	if breakdown := failureBreakdown(s.classes); breakdown != "" {
		fmt.Fprintf(&b, "\n**Failures by class:** %s\n", breakdown)
	}
	if breakdown := reasonBreakdown(s.prefixChangeReasons); breakdown != "" {
		fmt.Fprintf(&b, "\n**Cache resets by cause:** %s\n", breakdown)
	}

	notes := false
	for _, r := range results {
		if r.Note != "" {
			if !notes {
				fmt.Fprintf(&b, "\n<details><summary>Notes</summary>\n\n")
				notes = true
			}
			fmt.Fprintf(&b, "- `%s`: %s\n", r.ID, r.Note)
		}
	}
	if notes {
		fmt.Fprintf(&b, "\n</details>\n")
	}
	return b.String()
}

func renderTaskTable(b *strings.Builder, results []result) {
	fmt.Fprintf(b, "| Task | Result | Class | Steps | Tools | Time | Prompt | Completion | Cache hit | Cost |\n")
	fmt.Fprintf(b, "|------|--------|-------|------:|------:|-----:|-------:|-----------:|----------:|-----:|\n")
	for _, r := range results {
		if r.Skipped {
			fmt.Fprintf(b, "| `%s` | ⏭️ skipped | — | — | — | — | — | — | — | — |\n", r.ID)
			continue
		}
		res := "❌ fail"
		if r.Passed {
			res = "✅ pass"
		}
		name := fmt.Sprintf("`%s`", r.ID)
		if r.Attempt > 1 {
			name += fmt.Sprintf(" (try %d)", r.Attempt)
		}
		fmt.Fprintf(b, "| %s | %s | %s | %d | %d | %s | %s | %s | %s | %s%.4f |\n",
			name, res, r.class(), r.Steps, r.ToolCalls, dur(r.WallMs),
			comma(r.PromptTokens), comma(r.CompletionTokens),
			pct(r.CacheHitTokens, r.CacheHitTokens+r.CacheMissTokens),
			currencySym(r.Currency), r.Cost)
	}
	fmt.Fprintf(b, "\n<sub>Real provider run. Cache-hit %% is cached prompt tokens / total prompt tokens. Wall time is measured by the harness and includes process startup.</sub>\n")
}

func pct(n, d int) string {
	if d == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(d))
}

func costPerSolved(cost float64, solved int, currency string) string {
	if solved == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%s%.4f", currencySym(currency), cost/float64(solved))
}

func tokensPerSolved(tokens, solved int) string {
	if solved == 0 {
		return "n/a"
	}
	return comma(tokens / solved)
}

func median(ms []int64) int64 {
	if len(ms) == 0 {
		return 0
	}
	sorted := append([]int64(nil), ms...)
	slices.Sort(sorted)
	return sorted[len(sorted)/2]
}

func dur(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func failureBreakdown(classes map[string]int) string {
	names := make([]string, 0, len(classes))
	for name := range classes {
		if name != "solved" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s ×%d", name, classes[name]))
	}
	return strings.Join(parts, " · ")
}

// reasonBreakdown renders cache-prefix-change reason counts (compact_auto,
// snip, prune, tools, ...) the same way failureBreakdown renders failure
// classes, so a hit-rate regression in a PR shows which operation caused it.
func reasonBreakdown(reasons map[string]int) string {
	names := make([]string, 0, len(reasons))
	for name := range reasons {
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s ×%d", name, reasons[name]))
	}
	return strings.Join(parts, " · ")
}

func comma(n int) string {
	s := fmt.Sprint(n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func currencySym(c string) string {
	if c == "" {
		return ""
	}
	return c + " "
}
