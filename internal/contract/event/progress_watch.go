package event

// ProgressWatch is the host's reading of whether a run still moves: how many
// rounds have passed since the last effect it could observe, and how much input
// has been spent since then against the backstop. Content-free and advisory — the
// model never sees it, and it pauses nothing unless Pausing says so.
type ProgressWatch struct {
	Stalled       bool   // either limit holds; false clears an earlier report
	Cause         string // "rounds" or "tokens" while stalled
	IdleRounds    int
	RoundLimit    int
	PromptTokens  int // input spent since the last observable effect
	TokenLimit    int
	TokenMultiple int  // the context-window multiple TokenLimit came from
	Pausing       bool // the run ends on this report, under the user's pause setting
}

// Progress watch causes, wire-stable.
const (
	ProgressWatchCauseRounds        = "rounds"
	ProgressWatchCauseTokens        = "tokens"
	ProgressWatchCausePerseveration = "perseveration"
)
