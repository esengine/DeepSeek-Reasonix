package usage

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// ─── Text report ────────────────────────────────────────────────────────────

// PrintOverview prints the daily overview in a human-friendly terminal format.
func PrintOverview(w io.Writer, o Overview, models []ModelRow, label string) {
	fmt.Fprintf(w, "\n─── %s ──────────────────────────────────────\n\n", label)
	fmt.Fprintf(w, "  Requests    %s\n", commaFmt(o.Requests))
	fmt.Fprintf(w, "  Token   %s (输入 %s + 输出 %s)\n",
		commaFmt(o.TotalTokens), commaFmt(o.PromptTokens), commaFmt(o.CompletionTokens))
	rate := o.CacheHitRate()
	fmt.Fprintf(w, "  Cache    Hit %s / Miss %s → Rate %.1f%%\n",
		commaFmt(o.CacheHitTokens), commaFmt(o.CacheMissTokens), rate*100)
	fmt.Fprintf(w, "  Cost    %s%.4f\n", o.Currency, o.Cost)
	if o.Requests > 0 {
		fmt.Fprintf(w, "  TPM     %.0f\n", o.TPM)
		fmt.Fprintf(w, "  RPM     %.1f\n", o.RPM)
	}
	fmt.Fprintln(w)

	if len(models) > 0 {
		fmt.Fprintf(w, "─── 按模型 ───────────────────────────────────────\n\n")
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "  Model\tRequests\tInput\tOutput\tCache Hit\tCost\n")
		for _, m := range models {
			name := m.Model
			if m.Provider != "" {
				name = m.Provider + "/" + m.Model
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s%.4f\n",
				name, commaFmt(m.Requests),
				commaFmt(m.PromptTokens), commaFmt(m.CompletionTokens),
				commaFmt(m.CacheHitTokens), m.Currency, m.Cost)
		}
		tw.Flush()

		fmt.Fprintf(w, "\n─── Cache Efficiency ─────────────────────────────────────\n\n")
		for _, m := range models {
			total := m.CacheHitTokens + m.CacheMissTokens
			rate := 0.0
			if total > 0 {
				rate = float64(m.CacheHitTokens) / float64(total)
			}
			bar := progressBar(rate, 20)
			fmt.Fprintf(w, "  %-20s %s  %.1f%%\n", m.Model, bar, rate*100)
		}
	}
	fmt.Fprintln(w)
}

// PrintTrend prints the daily trend table with a mini bar chart.
func PrintTrend(w io.Writer, points []TrendPoint, label string) {
	fmt.Fprintf(w, "\n─── %s ─────────────────────────────────────\n\n", label)
	if len(points) == 0 {
		fmt.Fprintln(w, "  (no data)")
		return
	}
	// find max for scaling
	maxTokens := 0
	for _, p := range points {
		if p.TotalTokens > maxTokens {
			maxTokens = p.TotalTokens
		}
	}
	for _, p := range points {
		scale := 0.0
		if maxTokens > 0 {
			scale = float64(p.TotalTokens) / float64(maxTokens)
		}
		bar := progressBar(scale, 30)
		fmt.Fprintf(w, "  %s  %s  %10s  %s%.4f\n",
			p.Date[5:], bar, commaFmt(p.TotalTokens), p.Currency, p.Cost)
	}
	fmt.Fprintln(w)
}

// PrintLogs prints the tail log table.
func PrintLogs(w io.Writer, entries []LogEntry, label string) {
	fmt.Fprintf(w, "\n─── %s ─────────────────────────────────────────\n\n", label)
	if len(entries) == 0 {
		fmt.Fprintln(w, "  (no data)")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  Time\tModel\tSource\tInput\tOutput\tCache Hit\tCost\n")
	for _, e := range entries {
		ts := e.TS.Format("2006-01-02 15:04:05")
		name := e.Model
		if e.Provider != "" {
			name = e.Provider + "/" + e.Model
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s%.4f\n",
			ts, name, e.UsageSource,
			commaFmt(e.PromptTokens), commaFmt(e.CompletionTokens),
			commaFmt(e.CacheHitTokens), e.Currency, e.Cost)
	}
	tw.Flush()
	fmt.Fprintln(w)
}

// ─── JSON report ────────────────────────────────────────────────────────────

// PrintJSON writes the data as indented JSON.
func PrintJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// commaFmt formats an integer with thousands separators.
func commaFmt(n int) string {
	if n == 0 {
		return "0"
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre == 0 {
		pre = 3
	}
	b.WriteString(s[:pre])
	for i := pre; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// progressBar returns a filled bar like "████████░░░░" with the given fill ratio.
func progressBar(ratio float64, width int) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
