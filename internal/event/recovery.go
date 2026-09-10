package event

// RetryScope distinguishes connection+header retries, body-phase stream
// retries, and host-classified protocol recovery. Older clients ignore an
// unknown value and still render the generic retry state.
type RetryScope string

const (
	RetryScopeHeaders  RetryScope = "headers"
	RetryScopeStream   RetryScope = "stream"
	RetryScopeProtocol RetryScope = "protocol"
)

// RecoveryStatus is a local UI projection, never provider-visible metadata.
type RecoveryStatus struct {
	// State is the durable tool-recovery state (for example recovery_required).
	// It is local UI metadata and never provider-visible.
	State                string `json:"state,omitempty"`
	CallID               string `json:"call_id,omitempty"`
	AttemptID            string `json:"attempt_id,omitempty"`
	RequiresUserDecision bool   `json:"requires_user_decision,omitempty"`
	ReadOnly             bool   `json:"read_only,omitempty"`
	Phase                string `json:"phase,omitempty"`
	Reason               string `json:"reason,omitempty"`
	NextAttemptAt        int64  `json:"next_attempt_at,omitempty"`
	WaitedMs             int64  `json:"waited_ms,omitempty"`
	WaitBudgetMs         int64  `json:"wait_budget_ms,omitempty"`
	Waiting              bool   `json:"waiting,omitempty"`
}
