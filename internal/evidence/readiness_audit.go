package evidence

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
	CommandMismatchMissing    int
	MissingAcceptanceCriteria int
	MissingVerification       int
	MissingReview             int
	MissingSignoff            int
	MissingActionEvidence     int
	MissingMutation           int
	MissingCapabilities       int
}
