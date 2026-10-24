package eventwire

import "reasonix/internal/contract/event"

// WorkspaceLease is the JSON form of event.WorkspaceLease.
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

func toWireWorkspaceLease(l *event.WorkspaceLease) *WorkspaceLease {
	if l == nil {
		return nil
	}
	return &WorkspaceLease{
		ResourceKind:  l.ResourceKind,
		TransactionID: l.TransactionID, SessionID: l.SessionID, ExecutionID: l.ExecutionID,
		CallID: l.CallID, State: l.State, Reason: l.Reason, StartedAt: l.StartedAt,
		HolderTransactionID: l.HolderTransactionID, HolderExecutionID: l.HolderExecutionID,
		Contended: l.Contended, Reported: l.Reported,
		WaitedMs: l.WaitedMs, HeldMs: l.HeldMs, IdleMs: l.IdleMs,
		Holder: l.Holder, HolderSessionID: l.HolderSessionID,
		Paths: l.Paths, RequestedPaths: l.RequestedPaths, Repository: l.Repository,
	}
}

// wireLevel names a notice's severity. Only the warning tier is distinguished:
// everything else a frontend draws as information.
func wireLevel(level event.Level) string {
	if level == event.LevelWarn {
		return "warn"
	}
	return "info"
}
