package agent

import (
	"reasonix/internal/completion"
	"reasonix/internal/event"
	"reasonix/internal/taskcontract"
)

// completionReportAudit reduces a completion report to its content-free
// summary: counts, enums, and gap kinds, never a path or a command.
func completionReportAudit(rep completion.Report) event.CompletionReportAudit {
	audit := event.CompletionReportAudit{
		Verdict:        rep.Verdict.String(),
		Risk:           riskName(rep.Risk),
		Gaps:           len(rep.Gaps),
		GapKinds:       rep.GapKinds(),
		ClaimsVerified: len(rep.Claimed.Verified),
	}
	for _, gap := range rep.Gaps {
		if gap.Kind == completion.GapUnbackedClaim {
			audit.ClaimsUnbacked++
		}
	}
	for _, criterion := range rep.Criteria {
		if !criterion.Required {
			continue
		}
		audit.Criteria++
		if criterion.Status == taskcontract.Satisfied {
			audit.CriteriaSatisfied++
		}
	}
	audit.Changes = len(rep.Changes)
	for _, change := range rep.Changes {
		if !change.Reviewed {
			audit.ChangesUnreviewed++
		}
	}
	audit.Verifications = len(rep.Verifications)
	for _, verification := range rep.Verifications {
		switch {
		case !verification.Passed:
			audit.VerificationsFailed++
		case verification.Stale:
			audit.VerificationsStale++
		}
	}
	return audit
}

// completionReceipt projects the report onto the delivery shape. Runs with
// nothing to judge return nil rather than an empty card: a receipt that says
// nothing is noise, and noise is what makes people stop reading receipts.
func completionReceipt(rep completion.Report) *event.CompletionReceipt {
	if rep.Verdict == completion.VerdictUnknown {
		return nil
	}
	out := &event.CompletionReceipt{Verdict: rep.Verdict.String(), Risks: rep.Risks}
	for _, change := range rep.Changes {
		out.Changes = append(out.Changes, event.ReceiptChange{Path: change.Path, Reviewed: change.Reviewed})
	}
	for _, v := range rep.Verifications {
		out.Verifications = append(out.Verifications, event.ReceiptVerification{Command: v.Command, Passed: v.Passed, Stale: v.Stale})
	}
	for _, gap := range rep.Gaps {
		out.Gaps = append(out.Gaps, event.ReceiptGap{Kind: gap.Kind.String(), Detail: gap.Detail})
	}
	return out
}

func riskName(r taskcontract.Risk) string {
	switch r {
	case taskcontract.RiskHigh:
		return "high"
	case taskcontract.RiskMedium:
		return "medium"
	default:
		return "low"
	}
}
