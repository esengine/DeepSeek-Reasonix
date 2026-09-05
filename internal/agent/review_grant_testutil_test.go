package agent

import (
	"encoding/json"

	"reasonix/internal/evidence"
)

// reviewGrant is the grant a host issues to a worker contracted for one kind:
// it delivers that kind and may close exactly that obligation.
func reviewGrant(kind evidence.ReviewKind) ReviewReportGrant {
	return ReviewReportGrant{
		Delivery:  kind,
		Authority: evidence.GrantReviewAuthority(kind),
		Execution: "exec-" + string(kind),
	}
}

// grantedReviewReceipt is the receipt the host records for a report it accepted
// from a worker it granted that obligation to — the only shape a gate reads.
func grantedReviewReceipt(kind evidence.ReviewKind, args string) evidence.Receipt {
	authority := evidence.GrantReviewAuthority(kind)
	return evidence.Receipt{
		ToolName: "review_report", Success: true, Args: json.RawMessage(args),
		ReportKind: kind, ReviewAuthority: &authority, SourceExecutionID: "exec-" + string(kind),
	}
}
