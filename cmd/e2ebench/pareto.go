package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// paretoPoint is one arm on the accuracy-vs-TTCS plane. The product question
// is Pareto position, not averages: an arm beaten on both axes at once by the
// same competitor is the unambiguous alarm.
type paretoPoint struct {
	label       string
	acc         float64 // solved %
	ttcsMs      int64   // median time to correct solution
	solved, ran int
	dominatedBy string
}

func newParetoPoint(path string, s armStats) paretoPoint {
	return paretoPoint{
		label:  strings.TrimSuffix(filepath.Base(path), ".json"),
		acc:    solveRate(s.Solved, s.Ran),
		ttcsMs: median(s.TTCS),
		solved: s.Solved,
		ran:    s.Ran,
	}
}

// markDominated flags each point beaten on both axes by another (strictly on
// at least one). Points without a solve have no TTCS and cannot dominate.
func markDominated(points []paretoPoint) {
	for i := range points {
		for j := range points {
			if i == j || points[j].solved == 0 || points[i].solved == 0 {
				continue
			}
			betterAcc := points[j].acc >= points[i].acc
			betterTime := points[j].ttcsMs <= points[i].ttcsMs
			strict := points[j].acc > points[i].acc || points[j].ttcsMs < points[i].ttcsMs
			if betterAcc && betterTime && strict {
				points[i].dominatedBy = points[j].label
				break
			}
		}
	}
}

func paretoSection(points []paretoPoint) string {
	if len(points) < 2 {
		return ""
	}
	markDominated(points)
	var b strings.Builder
	b.WriteString("### Pareto: accuracy vs TTCS\n\n")
	b.WriteString("```\n" + paretoChart(points) + "```\n\n")
	for _, p := range points {
		switch {
		case p.solved == 0:
			fmt.Fprintf(&b, "- `%s`: no solves — off the chart\n", p.label)
		case p.dominatedBy != "":
			fmt.Fprintf(&b, "- ⚠️ `%s` is **dominated** by `%s`: at least as accurate and no slower — the unambiguous alarm\n", p.label, p.dominatedBy)
		default:
			fmt.Fprintf(&b, "- ✅ `%s` is on the Pareto frontier (%s solved, TTCS median %s)\n", p.label, pct(p.solved, p.ran), dur(p.ttcsMs))
		}
	}
	b.WriteString("\n")
	return b.String()
}

const (
	paretoRows = 9
	paretoCols = 46
)

// paretoChart renders the accuracy/TTCS scatter as fixed-width ASCII, letters
// keyed to a legend line. Dominated arms render as ✗ at their position.
func paretoChart(points []paretoPoint) string {
	charted := make([]paretoPoint, 0, len(points))
	for _, p := range points {
		if p.solved > 0 {
			charted = append(charted, p)
		}
	}
	if len(charted) == 0 {
		return "(no solved runs to chart)\n"
	}
	xmin, xmax, ymin := paretoBounds(charted)
	grid := make([][]rune, paretoRows)
	for r := range grid {
		grid[r] = []rune(strings.Repeat(" ", paretoCols))
	}
	legend := make([]string, 0, len(charted))
	for i, p := range charted {
		col := 0
		if xmax > xmin {
			col = int(float64(p.ttcsMs-xmin) / float64(xmax-xmin) * float64(paretoCols-1))
		}
		row := int((100 - p.acc) / (100 - ymin) * float64(paretoRows-1))
		marker := rune('A' + i)
		if p.dominatedBy != "" {
			marker = '✗'
		}
		grid[clampInt(row, 0, paretoRows-1)][clampInt(col, 0, paretoCols-1)] = marker
		legend = append(legend, fmt.Sprintf("%c=%s", 'A'+i, p.label))
	}
	var b strings.Builder
	b.WriteString("Accuracy\n")
	for r, line := range grid {
		switch r {
		case 0:
			fmt.Fprintf(&b, "%5s |%s\n", "100%", string(line))
		case paretoRows - 1:
			fmt.Fprintf(&b, "%5s |%s\n", fmt.Sprintf("%.0f%%", ymin), string(line))
		default:
			fmt.Fprintf(&b, "      |%s\n", string(line))
		}
	}
	fmt.Fprintf(&b, "      +%s→ TTCS\n", strings.Repeat("-", paretoCols))
	fmt.Fprintf(&b, "       %-*s%s\n", paretoCols-len(dur(xmax)), dur(xmin), dur(xmax))
	fmt.Fprintf(&b, "       %s\n", strings.Join(legend, "  "))
	return b.String()
}

// paretoBounds pads the time axis and floors the accuracy axis one decade
// under the worst arm so points sit inside the frame, not on its edges.
func paretoBounds(points []paretoPoint) (xmin, xmax int64, ymin float64) {
	xmin, xmax, ymin = points[0].ttcsMs, points[0].ttcsMs, points[0].acc
	for _, p := range points[1:] {
		xmin = min(xmin, p.ttcsMs)
		xmax = max(xmax, p.ttcsMs)
		ymin = min(ymin, p.acc)
	}
	pad := max((xmax-xmin)/10, 500)
	xmin = max(xmin-pad, 0)
	xmax += pad
	ymin = max(float64(int(ymin/10)*10-10), 0)
	if ymin >= 100 {
		ymin = 90
	}
	return xmin, xmax, ymin
}

func clampInt(v, lo, hi int) int {
	return min(max(v, lo), hi)
}
