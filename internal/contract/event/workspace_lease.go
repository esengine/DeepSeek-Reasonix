package event

// WorkspaceLease projects a claim wait or a closed contention account.
// Paths names the blocking extent; RequestedPaths names the granted or
// requested extent, including the workspace root for a whole-workspace claim.
type WorkspaceLease struct {
	ResourceKind        string   `json:"resourceKind,omitempty"`
	TransactionID       string   `json:"transactionId,omitempty"`
	SessionID           string   `json:"sessionId,omitempty"`
	ExecutionID         string   `json:"executionId,omitempty"`
	CallID              string   `json:"callId,omitempty"`
	State               string   `json:"state,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	StartedAt           string   `json:"startedAt,omitempty"`
	HolderTransactionID string   `json:"holderTransactionId,omitempty"`
	HolderExecutionID   string   `json:"holderExecutionId,omitempty"`
	Contended           int      `json:"contended"`
	Reported            int      `json:"reported,omitempty"`
	WaitedMs            int64    `json:"waitedMs,omitempty"`
	HeldMs              int64    `json:"heldMs"`
	IdleMs              int64    `json:"idleMs"`
	Holder              string   `json:"holder,omitempty"`
	HolderSessionID     string   `json:"holderSessionId,omitempty"`
	Paths               []string `json:"paths,omitempty"`
	RequestedPaths      []string `json:"requestedPaths,omitempty"`
	Repository          string   `json:"repository,omitempty"`
}
