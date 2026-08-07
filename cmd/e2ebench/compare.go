package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
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
	Ran, Solved, AccountedSolved       int
	Steps, Tools, Rounds, PlannerCalls int
	Tokens                             int
	Cost                               float64
	WallMs                             int64
	ByClass                            map[string]classStats
}

type classStats struct {
	Ran, Solved int
	WallMs      int64
}

func aggregateArm(results []result) armStats {
	s := armStats{ByClass: map[string]classStats{}}
	for _, r := range results {
		if r.Skipped {
			continue
		}
		s.Ran++
		if r.Passed {
			s.Solved++
		}
		label := r.Class
		if label == "" {
			label = "unclassified"
		}
		c := s.ByClass[label]
		c.Ran++
		if r.Passed {
			c.Solved++
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
		s.Cost += r.Cost
		s.WallMs += r.WallMs
		s.PlannerCalls += r.UsageBySource["planner"].Calls
		if r.Trajectory != nil {
			s.Rounds += r.Trajectory.ModelRounds
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
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "compare mode wants two -json report files: e2ebench -mode compare a.json b.json")
		os.Exit(2)
	}
	report, err := compareReports(flag.Arg(0), flag.Arg(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "compare:", err)
		os.Exit(1)
	}
	emit(report, outMD, "")
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
	labels := []string{pathA, pathB}
	for _, path := range labels {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		var results []result
		if err := json.Unmarshal(data, &results); err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		arms = append(arms, aggregateArm(results))
	}
	a, bStats := arms[0], arms[1]
	var b strings.Builder
	fmt.Fprintf(&b, "## e2ebench A/B: `%s` vs `%s`\n\n", pathA, pathB)
	fmt.Fprintf(&b, "| Metric | A | B |\n|---|---:|---:|\n")
	fmt.Fprintf(&b, "| Solved | %d/%d (%s) | %d/%d (%s) |\n", a.Solved, a.Ran, pct(a.Solved, a.Ran), bStats.Solved, bStats.Ran, pct(bStats.Solved, bStats.Ran))
	fmt.Fprintf(&b, "| Model requests / solved | %s | %s |\n", perSolved(float64(a.Steps), a.AccountedSolved), perSolved(float64(bStats.Steps), bStats.AccountedSolved))
	fmt.Fprintf(&b, "| Planner requests / solved | %s | %s |\n", perSolved(float64(a.PlannerCalls), a.AccountedSolved), perSolved(float64(bStats.PlannerCalls), bStats.AccountedSolved))
	fmt.Fprintf(&b, "| Model rounds / solved | %s | %s |\n", perSolved(float64(a.Rounds), a.AccountedSolved), perSolved(float64(bStats.Rounds), bStats.AccountedSolved))
	fmt.Fprintf(&b, "| Tool calls / solved | %s | %s |\n", perSolved(float64(a.Tools), a.AccountedSolved), perSolved(float64(bStats.Tools), bStats.AccountedSolved))
	fmt.Fprintf(&b, "| Tokens / solved | %s | %s |\n", perSolved(float64(a.Tokens), a.AccountedSolved), perSolved(float64(bStats.Tokens), bStats.AccountedSolved))
	fmt.Fprintf(&b, "| Wall seconds / solved | %s | %s |\n", perSolved(float64(a.WallMs)/1000, a.AccountedSolved), perSolved(float64(bStats.WallMs)/1000, bStats.AccountedSolved))
	fmt.Fprintf(&b, "| Cost / solved | %s | %s |\n", perSolved(a.Cost, a.AccountedSolved), perSolved(bStats.Cost, bStats.AccountedSolved))
	b.WriteString(marginalUtilitySection(a, bStats))
	b.WriteString("\n<sub>Per-solved figures divide each arm's accounted totals (failures included) by its accounted solves.</sub>\n")
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
