package main

import "fmt"

// renderDelegation prices what delegation actually bought. Every figure is
// host-recorded, so the section answers the only question that matters when an
// arm costs more: did the extra agents produce verified work, or just tokens.
func renderDelegation(results []result) string {
	var runs, nested, childCalls, parentCalls, mutations, dupes int
	var reports, prose, falseDone, downgrades, violations int
	var scopeHints, namedFiles, evidencePaths, discoveredPaths int
	solved, total, childTokens := 0, 0, 0
	for _, r := range results {
		if r.Skipped {
			continue
		}
		total++
		if r.Passed {
			solved++
		}
		runs += r.SubagentRuns
		nested += r.SubagentNestedRuns
		childCalls += r.SubagentToolCalls
		parentCalls += r.ToolCalls - r.SubagentToolCalls
		mutations += r.SubagentMutations
		dupes += r.DuplicateWorkPaths
		reports += r.CompletionReports
		prose += r.CompletionsProsedOnly
		falseDone += r.FalseCompletions
		downgrades += r.CriterionDowngrades
		violations += r.WriteScopeViolations
		scopeHints += r.ParentScopeHints
		namedFiles += r.ParentNamedFiles
		evidencePaths += r.ChildEvidencePaths
		discoveredPaths += r.ChildDiscoveredPaths
		if u, ok := r.UsageBySource["subagent"]; ok {
			childTokens += u.PromptTokens + u.CompletionTokens
		}
	}
	if runs == 0 {
		// A single-agent arm is a legitimate result, not a missing section: say
		// so, because an empty section reads as "not measured".
		if total == 0 {
			return ""
		}
		return fmt.Sprintf("**Delegation**: none — %d/%d solved by a single agent\n\n", solved, total)
	}

	b := fmt.Sprintf("**Delegation**: **%d** child runs (%d nested) · solved %d/%d (%s)\n",
		runs, nested, solved, total, pct(solved, total))
	b += fmt.Sprintf("- work split: parent **%d** tool calls · children **%d** (%s) · child mutations **%d**\n",
		parentCalls, childCalls, pct(childCalls, parentCalls+childCalls), mutations)
	// Cumulative prompt tokens over a child's own model calls, so the same
	// context counts once per call. Labelled as such: it is not fresh material.
	if childTokens > 0 {
		b += fmt.Sprintf("- child context re-sent: %s tokens per child, cumulative over its calls (%s across %d runs)\n",
			comma(childTokens/runs), comma(childTokens), runs)
	}
	// Scope and named files are reported apart and as counts. Narrowing the
	// search is what delegating costs; naming the file is handing over the
	// answer, and one number would report the cheap one as the expensive one.
	if evidencePaths > 0 {
		b += fmt.Sprintf("- evidence origin: children found **%s** of what they looked at themselves (%d/%d paths)\n",
			pct(discoveredPaths, evidencePaths), discoveredPaths, evidencePaths)
		b += fmt.Sprintf("- parent delegation text: **%d** scope hint(s) · **%d** file(s) named outright\n",
			scopeHints, namedFiles)
	} else {
		b += "- evidence origin: not scored — no child receipt carried a path\n"
	}
	if dupes > 0 {
		b += fmt.Sprintf("- **duplicate work**: %d file(s) mutated by more than one child\n", dupes)
	}
	closed := reports + prose
	b += fmt.Sprintf("- closed with a checkable claim: **%d/%d** (%s)\n", reports, closed, pct(reports, closed))
	if falseDone > 0 {
		b += fmt.Sprintf("- **false completions**: %d run(s), %d criterion claim(s) the host refused\n", falseDone, downgrades)
	}
	if violations > 0 {
		b += fmt.Sprintf("- **write-scope violations**: %d\n", violations)
	}
	return b + "\n"
}
