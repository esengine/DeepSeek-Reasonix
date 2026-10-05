// dose.go — does the cost step where the cue disappears?
package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Not "a bigger index is better" — CueDirectRead says the addresses are rarely
// used. The hypothesis is that a visible action cue lowers retrieval cost,
// which predicts a step at each task's own boundary, not a slope.

// scaleOrder is the dose axis, widest first.
var scaleOrder = []string{"default", "half", "quarter", "off"}

// boundaryStep is the index into the three transitions where a tier's cue
// disappears: default->half is 0, half->quarter is 1, quarter->off is 2.
func boundaryStep(tier string) int {
	switch tier {
	case tierDefault:
		return 0
	case tierHalf:
		return 1
	case tierQuarter:
		return 2
	default:
		return -1
	}
}

// doseCurve is one task's readings across the four budgets.
type doseCurve struct {
	Task   string
	Tier   string
	Metric string
	Values [4]float64
	// Missing marks arms that produced nothing. Printing those as zero draws a
	// curve falling to the floor where in fact it was never measured.
	Missing [4]bool
	Present bool
	// PreMean averages the arms where the cue is visible, PostMean the rest.
	PreMean, PostMean float64
	BoundaryDelta     float64
	// BoundaryAligned: the largest transition is the one where this task's cue
	// disappears. A change elsewhere is variance shaped like an effect.
	BoundaryAligned bool
}

func buildCurve(task, tier, metric string, byScale map[string]float64) doseCurve {
	c := doseCurve{Task: task, Tier: tier, Metric: metric, Present: true}
	for i, scale := range scaleOrder {
		v, ok := byScale[scale]
		if !ok {
			c.Present, c.Missing[i] = false, true
		}
		c.Values[i] = v
	}
	step := boundaryStep(tier)
	if !c.Present || step < 0 {
		return c
	}
	visible := step + 1 // arms before the boundary still show the cue
	c.PreMean = mean(c.Values[:visible])
	c.PostMean = mean(c.Values[visible:])
	c.BoundaryDelta = c.PostMean - c.PreMean

	transitions := [3]float64{
		math.Abs(c.Values[1] - c.Values[0]),
		math.Abs(c.Values[2] - c.Values[1]),
		math.Abs(c.Values[3] - c.Values[2]),
	}
	largest, at := 0.0, -1
	for i, t := range transitions {
		if t > largest {
			largest, at = t, i
		}
	}
	// A flat curve has no largest transition to align with anything.
	c.BoundaryAligned = largest > 0 && at == step
	return c
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return (s[mid-1] + s[mid]) / 2
}

// doseMetrics are the readings a curve is drawn for.
var doseMetrics = []struct {
	name string
	of   func(contextMetrics) float64
}{
	{"searches", func(m contextMetrics) float64 { return float64(m.SearchCalls) }},
	{"recall-tokens", func(m contextMetrics) float64 { return float64(m.RecallReturnedTokens) }},
	{"tool-rounds", func(m contextMetrics) float64 { return float64(m.RetrievalRounds + m.EscapeCalls) }},
	{"escapes", func(m contextMetrics) float64 { return float64(m.EscapeCalls) }},
}

// reportDoseResponse draws each task's curve and reports where the steps fell.
func reportDoseResponse(all []contextMetrics) string {
	valid := map[string]map[string]contextMetrics{}
	for _, m := range all {
		if m.Contaminated || m.FailureStage == "RunError" {
			continue
		}
		scale := strings.TrimPrefix(m.Arm, "index-")
		if valid[m.Task] == nil {
			valid[m.Task] = map[string]contextMetrics{}
		}
		valid[m.Task][scale] = m
	}

	var b strings.Builder
	b.WriteString("\n## Dose-response, per task\n")
	b.WriteString("  The arrow marks where this task's cue stops being addressed.\n")
	aligned := map[string]int{}
	counted := map[string]int{}
	deltas := map[string][]float64{}

	for _, t := range indexTasks() {
		arms := valid[t.ID]
		if len(arms) == 0 {
			continue
		}
		step := boundaryStep(t.CueTier)
		fmt.Fprintf(&b, "\n  %s (cue survives to %s)\n", t.ID, t.CueTier)
		fmt.Fprintf(&b, "    %-14s%s\n", "", scaleHeader(step))
		for _, metric := range doseMetrics {
			byScale := map[string]float64{}
			for scale, m := range arms {
				byScale[scale] = metric.of(m)
			}
			c := buildCurve(t.ID, t.CueTier, metric.name, byScale)
			mark := " "
			if c.BoundaryAligned {
				mark = "*"
				aligned[metric.name]++
			}
			if c.Present {
				counted[metric.name]++
				deltas[metric.name] = append(deltas[metric.name], c.BoundaryDelta)
			}
			delta := fmt.Sprintf("Δ%+8.1f", c.BoundaryDelta)
			if !c.Present {
				delta = "  no curve"
			}
			fmt.Fprintf(&b, "    %-14s%s   %s %s\n", metric.name, curveCells(c), delta, mark)
		}
	}

	b.WriteString("\n## Where the step fell (* = largest transition is at the cue boundary)\n")
	for _, metric := range doseMetrics {
		d := deltas[metric.name]
		sensitivity := ""
		if len(d) > 1 {
			sensitivity = fmt.Sprintf("  without the largest: %+.1f", meanWithoutLargest(d))
		}
		fmt.Fprintf(&b, "  %-14s boundary-aligned %d/%d   mean Δ%+.1f   median Δ%+.1f%s\n",
			metric.name, aligned[metric.name], counted[metric.name], mean(d), median(d), sensitivity)
	}
	return b.String()
}

// meanWithoutLargest is the robustness check: the largest effect is reported
// and then set aside, so a conclusion never rests on one task alone.
func meanWithoutLargest(d []float64) float64 {
	if len(d) < 2 {
		return mean(d)
	}
	worst, at := 0.0, 0
	for i, v := range d {
		if math.Abs(v) > worst {
			worst, at = math.Abs(v), i
		}
	}
	rest := append(append([]float64(nil), d[:at]...), d[at+1:]...)
	return mean(rest)
}

func scaleHeader(step int) string {
	var b strings.Builder
	for i, s := range scaleOrder {
		if i == step+1 {
			s = "↓" + s
		}
		fmt.Fprintf(&b, "%8s", s)
	}
	return b.String()
}

// curveCells prints a reading per arm, and a dash where an arm produced none.
func curveCells(c doseCurve) string {
	var b strings.Builder
	for i, v := range c.Values {
		if c.Missing[i] {
			fmt.Fprintf(&b, "%8s", "-")
			continue
		}
		fmt.Fprintf(&b, "%8.0f", v)
	}
	return b.String()
}

func percentile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	i := int(float64(len(s)-1) * q)
	return s[i]
}

func maxOf(xs []float64) float64 {
	out := 0.0
	for _, x := range xs {
		if x > out {
			out = x
		}
	}
	return out
}
