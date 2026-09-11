package agent

// Session returns the agent's current conversation, useful for persistence
// hooks that need to read the message log between turns. sessMu serialises this
// pointer read against SetSession, so a frontend (serve's concurrent /history and
// /new handlers) can't race the swap. The run loop touches a.session directly and
// only swaps it via SetSession while idle, so its reads need no lock.
func (a *Agent) Session() *Session {
	a.sess.mu.Lock()
	defer a.sess.mu.Unlock()
	return a.sess.conversation
}

// SetSession replaces the agent's conversation wholesale. Used by
// `reasonix --resume` to load a saved JSONL transcript before the first turn,
// so the model picks up exactly where it left off. Callers serialise it against a
// running turn (it only fires while idle); sessMu guards the pointer swap itself.
func (a *Agent) SetSession(s *Session) {
	a.sess.reset(s)
	a.resetPinnedContextState()
	// The replaced conversation's task is over, but the ledger and the bill
	// answer to beginRunTurn's scope check rather than to this seam.
	a.task.repeatFailures = nil
	a.task.repeatScope = ""
	a.pending.preserveEvidence = false
	a.pending.finalReadinessRecovery = false
	a.pending.finalReadinessRecoveryPrepared = false
	if s != nil {
		a.rebuildTodoState(s.Snapshot())
	}
}
