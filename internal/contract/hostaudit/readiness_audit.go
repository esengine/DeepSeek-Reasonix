package hostaudit

// ReadinessAuditResult classifies one host final-answer readiness audit receipt.
type ReadinessAuditResult string

const (
	ReadinessAllowed ReadinessAuditResult = "allowed"
	ReadinessBlocked ReadinessAuditResult = "blocked"
	ReadinessErrored ReadinessAuditResult = "errored"
)

// ReadinessAudit is a structured, non-rendered receipt for the final-answer
// readiness gate. It lets metrics sinks count why the host blocked a final
// answer without scraping Notice text or adding model-visible state.
type ReadinessAudit struct {
	Result                    ReadinessAuditResult
	Recovered                 bool
	MissingProjectChecks      int
	IncompleteTodos           int
	MissingAcceptanceCriteria int
	MissingVerification       int
	// Structured review and changed-path inspection are different debts read
	// from different indices, and one counter could not say which was owed.
	MissingStructuredReview int
	MissingPathInspection   int
	MissingSignoff          int
	MissingMutation         int
	MissingCapabilities     int
	MissingRender           int
}
