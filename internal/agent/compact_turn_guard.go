package agent

// sameTurnCompactionBlocked keeps ordinary pressure maintenance to one summary
// per active turn. A physical overflow may compact again only after the prior
// checkpoint made real progress and new provider-visible input has accumulated.
func (a *Agent) sameTurnCompactionBlocked(activeTurn int64, trigger string, mustFree bool, state CompactionState, inputHash string) bool {
	if activeTurn == 0 || trigger == CompactionTriggerManual {
		return false
	}
	if a.sess.compaction.lastTurn.Load() != activeTurn {
		return false
	}
	if !mustFree {
		return true
	}
	r := state.LastReceipt
	return r == nil ||
		r.Status != "applied" ||
		r.Action != "summary" ||
		r.ProjectionVersion != state.Projection.ProjectionVersion ||
		r.SavedTokens <= 0 ||
		r.OutputHash == "" ||
		inputHash == "" ||
		inputHash == r.OutputHash
}
