package main

import (
	"fmt"
	"strings"
)

// renderMatrix prints one table per arm. It reports what each row's value was
// on both sides of the process boundary, because a verdict without the two
// readings behind it is an assertion.
func renderMatrix(results []armResult, work string) string {
	var b strings.Builder
	b.WriteString("# Runtime resurrection matrix\n\n")
	b.WriteString("Measurement boundary: OS process. Phase B loads and inspects; it never calls a model.\n\n")
	fmt.Fprintf(&b, "Arm roots: %s\n\n", work)
	for _, res := range results {
		fmt.Fprintf(&b, "## arm: %s\n\n%s\n\n", res.Arm, res.Asks)
		if res.Invalid != "" {
			fmt.Fprintf(&b, "**ARM INVALID — %s.** Its rows below establish nothing.\n\n", res.Invalid)
		}
		b.WriteString("| Semantic | Before death | After new process | Verdict | Authority | Artifact |\n")
		b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
		for _, r := range res.Rows {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
				r.Semantic, cell(r.Before), cell(r.After), r.Verdict, r.Authority, r.Artifact)
		}
		b.WriteString("\n")
		b.WriteString(summarize(res))
		b.WriteString("\n")
	}
	return b.String()
}

func summarize(res armResult) string {
	counts := map[string]int{}
	for _, r := range res.Rows {
		counts[r.Verdict]++
	}
	var parts []string
	for _, v := range []string{verdictPersisted, verdictExact, verdictLossy, verdictLost, verdictNotMeasured, verdictChanged, verdictStable} {
		if counts[v] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", v, counts[v]))
		}
	}
	return strings.Join(parts, "  ") + "\n"
}

func cell(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	s = strings.ReplaceAll(s, "|", "\\|")
	if len(s) > 64 {
		return s[:61] + "..."
	}
	return s
}
