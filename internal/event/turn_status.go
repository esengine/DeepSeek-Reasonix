package event

// TurnStatus is the authoritative lifecycle of one top-level turn.
type TurnStatus string

const (
	TurnQueued      TurnStatus = "queued"
	TurnInProgress  TurnStatus = "in_progress"
	TurnWaitingUser TurnStatus = "waiting_user"
	TurnCancelling  TurnStatus = "cancelling"
	TurnCompleted   TurnStatus = "completed"
	TurnInterrupted TurnStatus = "interrupted"
	TurnFailed      TurnStatus = "failed"
	// TurnProtocolFailed is retained for replaying ledgers written by releases
	// that required the model-visible finish tool. New turns do not emit it.
	TurnProtocolFailed TurnStatus = "protocol_failed"
)

func (s TurnStatus) Terminal() bool {
	switch s {
	case TurnCompleted, TurnInterrupted, TurnFailed, TurnProtocolFailed:
		return true
	default:
		return false
	}
}
