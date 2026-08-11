package agent

// runLoopState holds sequential state for one Agent.Run invocation.
type runLoopState struct {
	runMaxSteps       int
	runMaxStepsKey    string
	runLimitHostOwned bool

	emptyFinalBlocks   int
	handoffNudges      int
	usedAnyTool        bool
	contextToolRepairs int
	graceRound         bool
	recoveryGraceRound bool

	todoProgress         int
	trackingTodoProgress bool
	todoStallRounds      int
	seenTodoProgress     map[string]struct{}

	executorHandoff bool
	input           string
	workDurationMs  func() int64

	// budget is the turn's spend axis: tokens, money, wall clock.
	budget runBudget
	// landCause records why the grace round was armed, so the pause the Run
	// ends with names the axis that actually stopped it.
	landCause landCause
}
