package agent

import (
	"encoding/json"

	"reasonix/internal/provider"
)

// runLoopState holds per-Run loop counters and flags. It is package-private and
// not shared across goroutines, preserving the sequential turn state machine.
type runLoopState struct {
	runMaxSteps       int
	runMaxStepsKey    string
	runLimitHostOwned bool

	emptyFinalBlocks         int
	requireVisibleFinal      bool
	visibleFinalRepair       bool
	visibleFinalRepairRounds int
	handoffNudges            int
	usedAnyTool              bool
	contextToolRepairs       int
	graceRound               bool
	recoveryGraceRound       bool

	todoProgress         int
	trackingTodoProgress bool
	todoStallRounds      int
	seenTodoProgress     map[string]struct{}

	executorHandoff bool
	// input is the user turn text after withTurnPreferences (used by handoff
	// nudges that inspect the original request wording).
	input string

	workDurationMs func() int64
}

func (s *runLoopState) shouldSample(step int) bool {
	return s.runMaxSteps <= 0 || step < s.runMaxSteps || s.graceRound || s.recoveryGraceRound || s.visibleFinalRepair
}

func (s *runLoopState) beginSamplingRound() {
	if s.visibleFinalRepair {
		s.visibleFinalRepairRounds++
	}
}

// streamedTurn is one provider completion collected by stream.
type streamedTurn struct {
	text               string
	reasoning          string
	signature          string
	reasoningID        string
	reasoningStatus    string
	calls              []provider.ToolCall
	responsesItems     []json.RawMessage
	usage              *provider.Usage
	interrupted        bool
	partialToolStarted bool
	partialCalls       []provider.ToolCall
	maxArgChars        int
	err                error
}
