package evidence

// Operation metric names. They are wire-stable counter identifiers: a sink
// aggregates them, so renaming one silently breaks whatever is watching it.
const (
	MetricOperationSettled         = "operation_settled_total"
	MetricOperationNeedsUser       = "operation_needs_user_total"
	MetricOperationRecoveryAttempt = "operation_recovery_attempt_total"
	MetricOperationDuplicateBlock  = "operation_duplicate_block_total"
	MetricVerificationAutoAttached = "verification_auto_attached_total"
	MetricVerificationUnclassified = "verification_unclassified_total"
	MetricReadSourceChanged        = "read_source_changed_total"
	MetricCompleteStepOptionalCall = "complete_step_optional_call_total"
)

// OperationAudit is one content-free observation of an operation transition.
// It carries the host's own identifiers only: no paths, arguments, command
// text, or tool output.
type OperationAudit struct {
	Metric          string         `json:"metric"`
	OperationID     string         `json:"operation_id,omitempty"`
	Tool            string         `json:"tool,omitempty"`
	State           OperationState `json:"state,omitempty"`
	FailureCode     string         `json:"failure_code,omitempty"`
	RecoveryAttempt int            `json:"recovery_attempt,omitempty"`
}
