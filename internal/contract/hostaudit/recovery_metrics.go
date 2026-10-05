package hostaudit

// RecoveryMetrics are content-free counters for release observation.
// They never record parameters, paths, or error bodies.
type RecoveryMetrics struct {
	FailureEvents      int64
	RuleContinues      int64
	ReviewContinues    int64
	HumanPrompts       int64
	HumanContinues     int64
	TaskGrantContinues int64
	TaskGrantUses      int64
	HumanRevises       int64
	ReviewErrors       int64
	ReviewLatencyMsSum int64
	ReviewLatencyCount int64
	RepeatPrompts      int64

	// Episode / generation counters (content-free).
	ModeResets               int64
	EpisodeRotations         int64
	StaleObservationsIgnored int64
}
