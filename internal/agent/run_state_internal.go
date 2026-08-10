package agent

// runLoopState holds sequential state for one Agent.Run invocation.
type runLoopState struct {
	runMaxSteps        int
	runMaxStepsKey     string
	runLimitHostOwned  bool
	runPauseAfterFinal bool

	emptyFinalBlocks    int
	handoffNudges       int
	usedAnyTool         bool
	contextToolRepairs  int
	graceRound          bool
	recoveryGraceRound  bool
	goalStuckGraceRound bool
	goalStuckLimit      int
	goalStuckKey        string
	goalStuckReason     string

	todoProgress         int
	trackingTodoProgress bool
	todoStallRounds      int
	seenTodoProgress     map[string]struct{}

	executorHandoff bool
	input           string
	workDurationMs  func() int64
}
