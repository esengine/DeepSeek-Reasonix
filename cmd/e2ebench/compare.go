package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// requestsBySourceLine breaks total model requests down by origin so an
// ablation arm shows exactly where its requests went (planner, subagents,
// compaction) instead of one opaque total.
func requestsBySourceLine(bySource map[string]sourceUsage) string {
	if len(bySource) == 0 {
		return ""
	}
	sources := make([]string, 0, len(bySource))
	for source, usage := range bySource {
		if usage.Calls > 0 {
			sources = append(sources, source)
		}
	}
	if len(sources) == 0 {
		return ""
	}
	sort.Slice(sources, func(i, j int) bool {
		if bySource[sources[i]].Calls != bySource[sources[j]].Calls {
			return bySource[sources[i]].Calls > bySource[sources[j]].Calls
		}
		return sources[i] < sources[j]
	})
	parts := make([]string, 0, len(sources))
	for _, source := range sources {
		usage := bySource[source]
		parts = append(parts, fmt.Sprintf("%s %s (%s tok)", source, comma(usage.Calls), comma(usage.PromptTokens+usage.CompletionTokens)))
	}
	return "**Requests by source:** " + strings.Join(parts, " · ") + "\n\n"
}

// armStats is one arm's aggregate over a -json report, using the same
// accounting conventions as renderBody: spend totals cover accounted runs
// (failures included) and per-solved figures divide by accounted solves.
type armStats struct {
	Ran, Pass1, Solved, AccountedSolved int
	Steps, Tools, Rounds, PlannerCalls  int
	Tokens, Hit, Miss                   int
	Cost                                float64
	WallMs                              int64
	FirstHit, FirstMiss                 int64
	Damaged, WithCorrect                int
	TTCS, TTFT                          []int64
	ByClass                             map[string]classStats
}

type classStats struct {
	Ran, Solved int
	WallMs      int64
	TTCS        []int64
}

func aggregateArm(results []result) armStats {
	s := armStats{ByClass: map[string]classStats{}}
	for _, r := range results {
		if r.Skipped {
			continue
		}
		// Retry entries share their task's denominator: only first attempts
		// count into Ran, matching renderBody's task-not-attempt convention.
		if r.Attempt <= 1 {
			s.Ran++
			if r.Passed {
				s.Pass1++
			}
		}
		if r.Passed {
			s.Solved++
			if r.TTCSMs > 0 {
				s.TTCS = append(s.TTCS, r.TTCSMs)
			} else {
				s.TTCS = append(s.TTCS, r.WallMs)
			}
		}
		label := r.Class
		if label == "" {
			label = "unclassified"
		}
		c := s.ByClass[label]
		if r.Attempt <= 1 {
			c.Ran++
		}
		if r.Passed {
			c.Solved++
			if r.TTCSMs > 0 {
				c.TTCS = append(c.TTCS, r.TTCSMs)
			} else {
				c.TTCS = append(c.TTCS, r.WallMs)
			}
		}
		c.WallMs += r.WallMs
		s.ByClass[label] = c
		if r.Unaccounted {
			continue
		}
		if r.Passed {
			s.AccountedSolved++
		}
		s.Steps += r.Steps
		s.Tools += r.ToolCalls
		s.Tokens += r.PromptTokens + r.CompletionTokens
		s.Hit += r.CacheHitTokens
		s.Miss += r.CacheMissTokens
		s.Cost += r.Cost
		s.WallMs += r.WallMs
		s.PlannerCalls += r.UsageBySource["planner"].Calls
		if r.Trajectory != nil {
			s.Rounds += r.Trajectory.ModelRounds
			if r.Trajectory.TTFTMs > 0 {
				s.TTFT = append(s.TTFT, r.Trajectory.TTFTMs)
			}
			s.FirstHit += r.Trajectory.FirstReqCacheHitTokens
			s.FirstMiss += r.Trajectory.FirstReqCacheMissTokens
		}
		if r.FirstCorrectMs > 0 {
			s.WithCorrect++
			if r.RegressedAfterCorrect {
				s.Damaged++
			}
		}
	}
	return s
}

func perSolved(total float64, solved int) string {
	if solved == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f", total/float64(solved))
}

func runCompareMode(outMD string) {
	if flag.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "compare mode wants two or more -json report files: e2ebench -mode compare a.json b.json [c.json ...]")
		os.Exit(2)
	}
	var report string
	var err error
	if flag.NArg() == 2 {
		report, err = compareReports(flag.Arg(0), flag.Arg(1))
	} else {
		report, err = multiCompareReport(flag.Args())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "compare:", err)
		os.Exit(1)
	}
	emit(report, outMD, "")
}

func loadArm(path string) (armStats, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return armStats{}, err
	}
	var results []result
	if err := json.Unmarshal(data, &results); err != nil {
		return armStats{}, fmt.Errorf("%s: %w", path, err)
	}
	return aggregateArm(results), nil
}

// multiCompareReport is the N-arm readout: one KPI row per arm, then the
// Pareto section — the question for a lineup is frontier position, not
// pairwise deltas.
func multiCompareReport(paths []string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "## e2ebench comparison: %d arms\n\n", len(paths))
	b.WriteString("| Arm | Pass@1 | Solved | TTFT | TTCS median | TTCS p90 | Solved/hour | 1st-req cache | Requests/solved | Tokens/solved | Cost/solved |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	points := make([]paretoPoint, 0, len(paths))
	arms := make([]armStats, 0, len(paths))
	for _, path := range paths {
		s, err := loadArm(path)
		if err != nil {
			return "", err
		}
		arms = append(arms, s)
		p := newParetoPoint(path, s)
		points = append(points, p)
		solvedPerHour := "—"
		if s.WallMs > 0 {
			solvedPerHour = fmt.Sprintf("%.1f", float64(s.Solved)*3_600_000/float64(s.WallMs))
		}
		cost := "—"
		if s.AccountedSolved > 0 {
			cost = fmt.Sprintf("%.4f", s.Cost/float64(s.AccountedSolved))
		}
		fmt.Fprintf(&b, "| `%s` | %s | %d/%d | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			p.label, pct(s.Pass1, s.Ran), s.Solved, s.Ran, durMs(median(s.TTFT)),
			dur(median(s.TTCS)), dur(pctile(s.TTCS, 90)), solvedPerHour,
			pct(int(s.FirstHit), int(s.FirstHit+s.FirstMiss)),
			perSolved(float64(s.Steps), s.AccountedSolved),
			tokensPerSolved(s.Tokens, s.AccountedSolved), cost)
	}
	b.WriteString("\n" + paretoSection(points))
	b.WriteString(perClassWinners(paths, arms))
	b.WriteString("<sub>Per-solved figures divide each arm's accounted totals (failures included) by its accounted solves; TTCS charges a retried solve with its failed attempts' wall.</sub>\n")
	return b.String(), nil
}

// perClassWinners is the routing readout: per task class, each arm's solve
// rate and TTCS median, and the winner (best solve rate, ties to the faster
// arm). A global default hides exactly this — the class that a leaner arm
// wins outright is a host-side routing opportunity, no classifier call needed.
func perClassWinners(paths []string, arms []armStats) string {
	classes := map[string]bool{}
	for _, a := range arms {
		for class := range a.ByClass {
			if class != "unclassified" {
				classes[class] = true
			}
		}
	}
	if len(classes) == 0 || len(arms) < 2 {
		return ""
	}
	names := make([]string, 0, len(classes))
	for class := range classes {
		names = append(names, class)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("### Per-class winners\n\n| Class |")
	labels := make([]string, len(paths))
	for i, path := range paths {
		labels[i] = strings.TrimSuffix(filepath.Base(path), ".json")
		fmt.Fprintf(&b, " `%s` |", labels[i])
	}
	b.WriteString(" Winner |\n|---|")
	b.WriteString(strings.Repeat("---:|", len(paths)) + "---|\n")
	for _, class := range names {
		fmt.Fprintf(&b, "| %s |", class)
		winner, bestSolve, bestTTCS := "—", -1.0, int64(0)
		for i, a := range arms {
			c := a.ByClass[class]
			if c.Ran == 0 {
				b.WriteString(" — |")
				continue
			}
			ttcs := median(c.TTCS)
			fmt.Fprintf(&b, " %s · %s |", pct(c.Solved, c.Ran), dur(ttcs))
			solve := float64(c.Solved) / float64(c.Ran)
			if solve > bestSolve || (solve == bestSolve && c.Solved > 0 && ttcs < bestTTCS) {
				winner, bestSolve, bestTTCS = labels[i], solve, ttcs
			}
		}
		fmt.Fprintf(&b, " %s |\n", winner)
	}
	return b.String() + "\n"
}

// accumulateSources folds one run's per-origin usage into the suite totals.
func accumulateSources(total map[string]sourceUsage, run map[string]sourceUsage) {
	for source, usage := range run {
		agg := total[source]
		agg.Calls += usage.Calls
		agg.PromptTokens += usage.PromptTokens
		agg.CompletionTokens += usage.CompletionTokens
		agg.Cost += usage.Cost
		total[source] = agg
	}
}

// compareReports renders an A/B delta table from two -json report files —
// the readout for an ablation experiment (e.g. control vs -ablate planner).
func compareReports(pathA, pathB string) (string, error) {
	arms := make([]armStats, 0, 2)
	for _, path := range []string{pathA, pathB} {
		s, err := loadArm(path)
		if err != nil {
			return "", err
		}
		arms = append(arms, s)
	}
	a, bStats := arms[0], arms[1]
	var b strings.Builder
	fmt.Fprintf(&b, "## e2ebench A/B: `%s` vs `%s`\n\n", pathA, pathB)
	fmt.Fprintf(&b, "| Metric | A | B |\n|---|---:|---:|\n")
	fmt.Fprintf(&b, "| Solved | %d/%d (%s) | %d/%d (%s) |\n", a.Solved, a.Ran, pct(a.Solved, a.Ran), bStats.Solved, bStats.Ran, pct(bStats.Solved, bStats.Ran))
	fmt.Fprintf(&b, "| Pass@1 | %s | %s |\n", pct(a.Pass1, a.Ran), pct(bStats.Pass1, bStats.Ran))
	fmt.Fprintf(&b, "| TTFT median | %s | %s |\n", durMs(median(a.TTFT)), durMs(median(bStats.TTFT)))
	fmt.Fprintf(&b, "| TTCS median | %s | %s |\n", dur(median(a.TTCS)), dur(median(bStats.TTCS)))
	fmt.Fprintf(&b, "| TTCS p90 | %s | %s |\n", dur(pctile(a.TTCS, 90)), dur(pctile(bStats.TTCS, 90)))
	fmt.Fprintf(&b, "| Cache hit | %s | %s |\n", pct(a.Hit, a.Hit+a.Miss), pct(bStats.Hit, bStats.Hit+bStats.Miss))
	fmt.Fprintf(&b, "| First-request cache hit | %s | %s |\n", pct(int(a.FirstHit), int(a.FirstHit+a.FirstMiss)), pct(int(bStats.FirstHit), int(bStats.FirstHit+bStats.FirstMiss)))
	fmt.Fprintf(&b, "| Overthinking damage | %s | %s |\n", pct(a.Damaged, a.WithCorrect), pct(bStats.Damaged, bStats.WithCorrect))
	fmt.Fprintf(&b, "| Model requests / solved | %s | %s |\n", perSolved(float64(a.Steps), a.AccountedSolved), perSolved(float64(bStats.Steps), bStats.AccountedSolved))
	fmt.Fprintf(&b, "| Planner requests / solved | %s | %s |\n", perSolved(float64(a.PlannerCalls), a.AccountedSolved), perSolved(float64(bStats.PlannerCalls), bStats.AccountedSolved))
	fmt.Fprintf(&b, "| Model rounds / solved | %s | %s |\n", perSolved(float64(a.Rounds), a.AccountedSolved), perSolved(float64(bStats.Rounds), bStats.AccountedSolved))
	fmt.Fprintf(&b, "| Tool calls / solved | %s | %s |\n", perSolved(float64(a.Tools), a.AccountedSolved), perSolved(float64(bStats.Tools), bStats.AccountedSolved))
	fmt.Fprintf(&b, "| Tokens / solved | %s | %s |\n", perSolved(float64(a.Tokens), a.AccountedSolved), perSolved(float64(bStats.Tokens), bStats.AccountedSolved))
	fmt.Fprintf(&b, "| Wall seconds / solved | %s | %s |\n", perSolved(float64(a.WallMs)/1000, a.AccountedSolved), perSolved(float64(bStats.WallMs)/1000, bStats.AccountedSolved))
	fmt.Fprintf(&b, "| Cost / solved | %s | %s |\n", perSolved(a.Cost, a.AccountedSolved), perSolved(bStats.Cost, bStats.AccountedSolved))
	b.WriteString(marginalUtilitySection(a, bStats))
	b.WriteString("\n" + paretoSection([]paretoPoint{newParetoPoint(pathA, a), newParetoPoint(pathB, bStats)}))
	b.WriteString("<sub>Per-solved figures divide each arm's accounted totals (failures included) by its accounted solves.</sub>\n")
	return b.String(), nil
}

func solveRate(solved, ran int) float64 {
	if ran == 0 {
		return 0
	}
	return float64(solved) * 100 / float64(ran)
}

func wallPerTask(wallMs int64, ran int) float64 {
	if ran == 0 {
		return 0
	}
	return float64(wallMs) / 1000 / float64(ran)
}

// marginalUtilitySection is the decision readout: not "does A help" but what
// each accuracy point costs in latency, overall and per task class, so a
// subsystem can be routed per class instead of globally defaulted.
func marginalUtilitySection(a, b armStats) string {
	var out strings.Builder
	fmt.Fprintf(&out, "\n**Marginal utility (A − B):** accuracy %+.1fpp · wall/task %+.1fs\n\n",
		solveRate(a.Solved, a.Ran)-solveRate(b.Solved, b.Ran),
		wallPerTask(a.WallMs, a.Ran)-wallPerTask(b.WallMs, b.Ran))
	classes := make([]string, 0, len(a.ByClass)+len(b.ByClass))
	seen := map[string]bool{}
	for _, m := range []map[string]classStats{a.ByClass, b.ByClass} {
		for class := range m {
			if !seen[class] {
				seen[class] = true
				classes = append(classes, class)
			}
		}
	}
	if len(classes) == 0 || (len(classes) == 1 && classes[0] == "unclassified") {
		return out.String()
	}
	sort.Strings(classes)
	out.WriteString("| Class | A solved | B solved | Δ accuracy | A wall/task | B wall/task | Δ wall |\n|---|---:|---:|---:|---:|---:|---:|\n")
	for _, class := range classes {
		ca, cb := a.ByClass[class], b.ByClass[class]
		fmt.Fprintf(&out, "| %s | %d/%d | %d/%d | %+.1fpp | %.1fs | %.1fs | %+.1fs |\n",
			class, ca.Solved, ca.Ran, cb.Solved, cb.Ran,
			solveRate(ca.Solved, ca.Ran)-solveRate(cb.Solved, cb.Ran),
			wallPerTask(ca.WallMs, ca.Ran), wallPerTask(cb.WallMs, cb.Ran),
			wallPerTask(ca.WallMs, ca.Ran)-wallPerTask(cb.WallMs, cb.Ran))
	}
	out.WriteString("\n")
	return out.String()
}
