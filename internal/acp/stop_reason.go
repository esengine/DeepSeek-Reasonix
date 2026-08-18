package acp

// ACP v1 stop reasons emitted by Reasonix.
const (
	StopEndTurn         StopReason = "end_turn"
	StopCancelled       StopReason = "cancelled"
	StopMaxTurnRequests StopReason = "max_turn_requests"
)
