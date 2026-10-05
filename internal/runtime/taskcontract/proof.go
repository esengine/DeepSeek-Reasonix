package taskcontract

import "reasonix/internal/safety/evidence"

// refFor classifies one receipt as the kind of proof it carries. For a
// verification, a shell reports one status for the whole command, so the host's
// reading of it — never the call's error — decides whether the check passed.
func refFor(epoch uint64, r evidence.Receipt) EvidenceRef {
	kind := EvidenceRead
	switch {
	case r.ToolName == "review_report":
		kind = EvidenceReview
	case r.Command != "" && evidence.ReceiptRunsVerification(r):
		kind = EvidenceVerification
	case r.Mutation || r.Write:
		kind = EvidenceMutation
	}
	success := r.Success
	if kind == EvidenceVerification {
		success = evidence.VerificationOutcome(r) == evidence.VerificationPassed
	}
	return EvidenceRef{Kind: kind, MutationEpoch: epoch, Source: r.ToolName, Success: success}
}

// proofRefuted reports whether the receipt disproves a check rather than merely
// failing to prove it: a verification whose exit status the shell hid leaves the
// check waiting instead of failing it.
func proofRefuted(r evidence.Receipt, ref EvidenceRef) bool {
	if ref.Kind == EvidenceVerification {
		return evidence.VerificationOutcome(r) == evidence.VerificationFailed
	}
	return !r.Success
}
