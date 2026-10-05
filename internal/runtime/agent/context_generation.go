package agent

// ContextGeneration identifies one build of the model-visible history. It
// changes whenever that history is rebuilt in a way that can drop what an
// earlier turn carried, so a latch that restates context keys on it.
type ContextGeneration struct {
	Rewrite    int    `json:"rewrite"`
	Projection uint64 `json:"projection"`
}

// ContextGeneration reports the current build. Compaction installs a projection
// rather than rewriting the log, so a latch keyed on the rewrite counter alone
// never notices a fold - and goes on pointing at context the fold removed.
func (a *Agent) ContextGeneration() ContextGeneration {
	if a == nil {
		return ContextGeneration{}
	}
	return a.window().contextGeneration()
}

func (a *contextWindow) contextGeneration() ContextGeneration {
	var rewrite int
	if session := a.Session(); session != nil {
		rewrite = session.RewriteVersion()
	}
	return ContextGeneration{Rewrite: rewrite, Projection: a.currentProjectionVersion()}
}
