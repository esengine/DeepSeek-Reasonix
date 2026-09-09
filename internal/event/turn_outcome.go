package event

const TurnOutcomeFinalReadiness = "final_readiness"

const TurnOutcomeIncompleteRead = "incomplete_read"

// TurnOutcomeRecoveryPaused marks an Auto recovery Episode budget stop. New
// clients show an informational status (not send-failed); older clients still
// read Err text and ignore the unknown outcome.
const TurnOutcomeRecoveryPaused = "recovery_paused"
