package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// fanoutMetrics mirrors internal/frontend/cli.RunMetrics's field of the same name: what
// a run's fan-outs cost in wall clock against what the same work costs one at a
// time. The agent folds it from the run graph, which is the only record of it.
type fanoutMetrics struct {
	Groups         int   `json:"fanout_groups,omitempty"`
	Workers        int   `json:"fanout_workers,omitempty"`
	Adopted        int   `json:"fanout_adopted,omitempty"`
	WallMs         int64 `json:"fanout_wall_ms,omitempty"`
	WorkMs         int64 `json:"fanout_work_ms,omitempty"`
	CriticalPathMs int64 `json:"fanout_critical_path_ms,omitempty"`
	SlotWaitMs     int64 `json:"fanout_slot_wait_ms,omitempty"`
	ClaimWaitMs    int64 `json:"fanout_claim_wait_ms,omitempty"`
}

// fanoutTotals sums an arm's fan-out pricing. A run that never fanned out adds
// nothing, so the figures describe the tasks that delegated rather than an
// average the single-agent tasks diluted.
func fanoutTotals(results []result) fanoutMetrics {
	var out fanoutMetrics
	for _, r := range results {
		if r.Skipped || r.Fanout == nil {
			continue
		}
		out.Groups += r.Fanout.Groups
		out.Workers += r.Fanout.Workers
		out.Adopted += r.Fanout.Adopted
		out.WallMs += r.Fanout.WallMs
		out.WorkMs += r.Fanout.WorkMs
		out.CriticalPathMs += r.Fanout.CriticalPathMs
		out.SlotWaitMs += r.Fanout.SlotWaitMs
		out.ClaimWaitMs += r.Fanout.ClaimWaitMs
	}
	return out
}

// renderFanout prices the shape. Finishing sooner is the only reason to run
// work side by side, so the section leads with work against wall and then
// separates the floor the declared dependencies impose from scheduling loss.
func renderFanout(results []result) string {
	t := fanoutTotals(results)
	if t.Groups == 0 {
		return ""
	}
	b := fmt.Sprintf("**Fan-out**: **%d** group(s) · **%d** member(s)", t.Groups, t.Workers)
	if t.Adopted > 0 {
		b += fmt.Sprintf(" · **%d** reused a finished answer", t.Adopted)
	}
	b += fmt.Sprintf("\n- parallelism: **%s** of work in **%s** of wall (**%s**)\n",
		dur(t.WorkMs), dur(t.WallMs), speedup(t.WorkMs, t.WallMs))
	b += fmt.Sprintf("- floor: critical path **%s** · scheduling loss **%s**\n",
		dur(t.CriticalPathMs), dur(t.WallMs-t.CriticalPathMs))
	if t.ClaimWaitMs > 0 {
		b += fmt.Sprintf("- claim wait: **%s** held ready behind a write path already claimed; no ceiling releases it\n", dur(t.ClaimWaitMs))
	}
	if t.SlotWaitMs > 0 {
		b += fmt.Sprintf("- slot wait: **%s** held ready at the concurrency ceiling\n", dur(t.SlotWaitMs))
	}
	return b + "\n"
}

// speedup is work over wall. At 1.00× the fan-out serialised: the graph was
// drawn, nothing overlapped, and the extra agents bought no time at all.
func speedup(work, wall int64) string {
	if wall <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f×", float64(work)/float64(wall))
}

// fanoutComparison is the arm-by-arm readout of what the shape bought. It is
// omitted unless an arm actually fanned out: a table of dashes would report a
// measurement where there was only a suite that never delegated.
func fanoutComparison(paths []string, arms []armStats) string {
	fanned := false
	for _, a := range arms {
		fanned = fanned || a.Fanout.Groups > 0
	}
	if !fanned || len(arms) < 2 {
		return ""
	}
	var b strings.Builder
	b.WriteString("### Fan-out\n\n| Arm | Groups | Members | Reused | Work | Wall | Speed-up | Critical path | Slot wait |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for i, a := range arms {
		f := a.Fanout
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %s | %s | %s | %s | %s |\n",
			strings.TrimSuffix(filepath.Base(paths[i]), ".json"),
			f.Groups, f.Workers, f.Adopted, dur(f.WorkMs), dur(f.WallMs),
			speedup(f.WorkMs, f.WallMs), dur(f.CriticalPathMs), dur(f.SlotWaitMs))
	}
	return b.String() + "\n"
}
