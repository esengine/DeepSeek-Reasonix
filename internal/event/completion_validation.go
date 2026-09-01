package event

import "reasonix/internal/nilutil"

const UsageSourceCompletionEvaluator = "completion-evaluator"

// TurnOutcomeCompletionUncertain marks a resumable stop after the completion
// validator could not confirm the turn's result. The result and completed work
// are kept; clients show an informational status, never a send failure.
const TurnOutcomeCompletionUncertain = "completion_uncertain"

// CompletionValidationInfo is the content-free audit of one completion
// validation: the configured mode, the validator's outcome, the attempt
// number within the run, the call duration, and an error class. It never
// carries the candidate text or the evaluator's reason.
type CompletionValidationInfo struct {
	Mode       string // off | shadow | enforce
	Outcome    string // complete | continue | needs_user | blocked | uncertain | error
	Attempt    int    // 1-based evaluation attempt within the run
	DurationMs int64
	ErrorClass string // timeout | invalid_output | unavailable | over_budget | error | ""; empty when Outcome is a verdict
}

// CompletionValidationAuditSink receives host-only completion validation
// telemetry. The audit deliberately stays outside Event so it cannot cross
// frontend, Serve, ACP, or persisted event-wire boundaries.
type CompletionValidationAuditSink interface {
	RecordCompletionValidation(CompletionValidationInfo)
}

// RecordCompletionValidation forwards a content-free completion validation
// audit to sinks that explicitly opt in.
func RecordCompletionValidation(s Sink, info CompletionValidationInfo) {
	if nilutil.IsNil(s) {
		return
	}
	if cs, ok := s.(CompletionValidationAuditSink); ok {
		cs.RecordCompletionValidation(info)
	}
}
