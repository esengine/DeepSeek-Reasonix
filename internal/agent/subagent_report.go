package agent

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/tool"
)

// hostReceiptsMaxItems bounds each rendered line so a long child run cannot
// crowd the parent's context out with paths and commands.
const hostReceiptsMaxItems = 8

const hostReceiptsHeader = "Host receipts (recorded by the host as the sub-agent ran, not claimed by it):"

const hostReceiptsViolationLabel = "OUTSIDE DECLARED write_paths"

// splitHostReceipts separates a child's own prose from the host attestation
// appended to it. Aggregates truncate prose to fit a budget; the attestation is
// bounded already and must never be the part that gets cut.
func splitHostReceipts(answer string) (prose, receipts string) {
	idx := strings.LastIndex(answer, hostReceiptsHeader)
	if idx < 0 {
		return answer, ""
	}
	return strings.TrimRight(answer[:idx], "\n"), strings.TrimSpace(answer[idx:])
}

// boundedHostReceipts trims an attestation to fit limit bytes. The header and
// any violation line always survive: a parent may lose the detail of what
// changed, but never the fact that a write left the declared claim.
func boundedHostReceipts(receipts string, limit int) string {
	if receipts == "" || len(receipts) <= limit {
		return receipts
	}
	lines := strings.Split(receipts, "\n")
	var violations []string
	for _, line := range lines[1:] {
		if strings.Contains(line, hostReceiptsViolationLabel) {
			violations = append(violations, line)
		}
	}
	if len(violations) == 0 {
		return utf8Prefix(lines[0], limit)
	}
	// A claim escape outranks the header it would normally sit under.
	if withHeader := strings.Join(append(lines[:1:1], violations...), "\n"); len(withHeader) <= limit {
		return withHeader
	}
	return utf8Prefix(strings.Join(violations, "\n"), limit)
}

// decorateExecutionReceipt records what the host itself observed about one tool
// call. The output length and the process outcome never come from model
// arguments, which is what makes the resulting attestation trustworthy.
func decorateExecutionReceipt(rec *evidence.Receipt, result string, ex *tool.ShellExecution) {
	if rec == nil {
		return
	}
	rec.OutputBytes = len(strings.TrimSpace(result))
	if ex == nil {
		return
	}
	if ex.ExitCode != nil {
		code := *ex.ExitCode
		rec.ExitCode = &code
	}
	rec.Verification = ex.Verification
}

// composeSubagentAnswer assembles everything the parent is shown for one child
// run: the host-adjudicated completion claim when the child submitted one, the
// child's own prose, then the host's receipts.
func composeSubagentAnswer(ctx context.Context, answer string, sub *Agent, claims WritePathSet, delegationText string) string {
	summary := sub.EvidenceSummary()
	report, reasons, hasReport := sub.CompletionReport()
	if hasReport {
		answer = strings.TrimSpace(formatCompletionReport(report, reasons) + "\n\n" + answer)
	}
	recordDelegationAudit(ctx, summary, claims, report, reasons, hasReport, delegationText)
	return appendHostReceipts(answer, summary, claims)
}

// recordDelegationAudit emits one structured receipt per child run. It reports
// what the host observed and what it refused to back, so an orchestration
// benchmark can separate real gains from extra tokens spent. delegationText is
// the parent-authored task before host framing, which is what makes the
// evidence-origin split a host record rather than a claim.
func recordDelegationAudit(ctx context.Context, summary evidence.ChildEvidenceSummary, claims WritePathSet, report evidence.CompletionReport, reasons []string, hasReport bool, delegationText string) {
	audit := evidence.DelegationAudit{
		Depth:           SubagentDepth(ctx),
		ToolCalls:       len(summary.Receipts),
		MutationPaths:   summary.MutationPaths(),
		ClaimViolations: len(claimViolations(summary, claims)),
		HasReport:       hasReport,
		Downgrades:      len(reasons),
	}
	audit.Mutations = len(audit.MutationPaths)
	audit.ClassifyEvidenceOrigin(delegationText, summary.EvidencePaths())
	if hasReport {
		audit.AdjudicatedStatus = string(report.Status)
	}
	_, sink, _, _ := CallContext(ctx)
	event.RecordDelegationAudit(sink, audit)
}

// formatCompletionReport renders the child's claim after the host has lowered
// whatever its receipts could not back. Downgrades are shown, never silently
// applied: a parent that cannot see the adjudication cannot trust the status.
func formatCompletionReport(report evidence.CompletionReport, reasons []string) string {
	var b strings.Builder
	b.WriteString("status: ")
	b.WriteString(string(report.Status))
	if len(reasons) > 0 {
		b.WriteString(" (lowered by the host: unbacked criterion claims)")
	}
	b.WriteString("\nsummary: ")
	b.WriteString(report.Summary)
	for _, c := range report.Criteria {
		b.WriteString("\n  " + c.ID + " " + string(c.Status))
		if proof := criterionProof(c); proof != "" {
			b.WriteString(" — " + proof)
		}
	}
	for _, reason := range reasons {
		b.WriteString("\n  host lowered " + reason)
	}
	for _, u := range report.Unresolved {
		b.WriteString("\nunresolved: " + u)
	}
	return b.String()
}

func criterionProof(c evidence.AcceptanceCriterion) string {
	var parts []string
	for _, e := range c.Evidence {
		switch {
		case strings.TrimSpace(e.Command) != "":
			parts = append(parts, e.Command)
		case len(e.Paths) > 0:
			parts = append(parts, strings.Join(e.Paths, " "))
		case strings.TrimSpace(e.Summary) != "":
			parts = append(parts, e.Kind+": "+e.Summary)
		}
	}
	return joinBoundedReceipts(parts)
}

// appendHostReceipts attaches the host's own attestation to a child's answer.
// A child cannot write, suppress, or contradict these lines. An empty block is
// omitted entirely, so read-only research children stay exactly as cheap as
// they were before.
func appendHostReceipts(answer string, summary evidence.ChildEvidenceSummary, claims WritePathSet) string {
	block := formatHostReceipts(summary, claims)
	if block == "" {
		return answer
	}
	if strings.TrimSpace(answer) == "" {
		return block
	}
	return answer + "\n\n" + block
}

// claimViolations returns the mutations the host observed outside the write
// claim the child declared. Tool-level confinement already refuses these, so a
// non-empty result means a write reached the workspace through a surface the
// claim could not bind — the parent must not treat the run as scoped.
func claimViolations(summary evidence.ChildEvidenceSummary, claims WritePathSet) []string {
	if claims.Empty() {
		return nil
	}
	var out []string
	for _, p := range summary.MutationPaths() {
		if !claims.AllowsPath(p) {
			out = append(out, p)
		}
	}
	return out
}

// formatHostReceipts renders only what the host is willing to attest to: files
// the child really changed, and commands whose outcome the host observed.
// Ordinary reads and greps are excluded on purpose — they are not claims a
// parent has to adjudicate, and every rendered line costs parent context.
func formatHostReceipts(summary evidence.ChildEvidenceSummary, claims WritePathSet) string {
	changed := summary.MutationPaths()
	commands := hostReceiptCommands(summary)
	violations := claimViolations(summary, claims)
	if len(changed) == 0 && len(commands) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(hostReceiptsHeader)
	if len(changed) > 0 {
		b.WriteString("\n  changed: ")
		b.WriteString(joinBoundedReceipts(changed))
	}
	if len(commands) > 0 {
		b.WriteString("\n  commands: ")
		b.WriteString(joinBoundedReceipts(commands))
	}
	if len(violations) > 0 {
		b.WriteString("\n  " + hostReceiptsViolationLabel + ": ")
		b.WriteString(joinBoundedReceipts(violations))
	}
	return b.String()
}

// hostReceiptCommands keeps only shell receipts carrying an outcome worth
// attesting: a host-classified verification, or a command that did not succeed.
func hostReceiptCommands(summary evidence.ChildEvidenceSummary) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range summary.Receipts {
		cmd := strings.TrimSpace(r.Command)
		outcome := hostReceiptOutcome(r)
		if cmd == "" || outcome == "" || seen[cmd] {
			continue
		}
		seen[cmd] = true
		out = append(out, cmd+outcome)
	}
	return out
}

func hostReceiptOutcome(r evidence.Receipt) string {
	var parts []string
	switch r.Verification {
	case evidence.VerificationPassed:
		parts = append(parts, "verification passed")
	case evidence.VerificationFailed:
		parts = append(parts, "verification failed")
	}
	switch {
	case r.ExitCode != nil && (*r.ExitCode != 0 || len(parts) > 0):
		parts = append(parts, fmt.Sprintf("exit %d", *r.ExitCode))
	case r.ExitCode == nil && !r.Success:
		parts = append(parts, "did not complete")
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func joinBoundedReceipts(items []string) string {
	if len(items) <= hostReceiptsMaxItems {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:hostReceiptsMaxItems], ", ") +
		fmt.Sprintf(" (+%d more)", len(items)-hostReceiptsMaxItems)
}
