package event

// TurnOutcomeCompletionUncertain is retained for old event readers. New runs
// no longer produce this outcome for completion validation; the value may
// still appear in persisted or cross-process events from older versions.
const TurnOutcomeCompletionUncertain = "completion_uncertain"
