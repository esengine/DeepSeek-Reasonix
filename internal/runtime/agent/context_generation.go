package agent

// ContextGeneration identifies one build of the model-visible history that a
// latch restating context keys on. Only a compaction install changes it:
// the session's rewrite counter also moves for local bookkeeping (a tool call's
// diff, a decision receipt, resolved proxy metadata) that never reaches the
// provider, so keying on it re-arms the latch on nearly every tool round.
type ContextGeneration struct {
	Projection uint64 `json:"projection"`
}

// ContextGeneration reports the current build.
func (a *Agent) ContextGeneration() ContextGeneration {
	if a == nil {
		return ContextGeneration{}
	}
	return a.window().contextGeneration()
}

func (a *contextWindow) contextGeneration() ContextGeneration {
	return ContextGeneration{Projection: a.currentProjectionVersion()}
}
