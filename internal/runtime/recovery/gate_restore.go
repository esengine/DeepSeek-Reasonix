package recovery

import "fmt"

// Restore loads persisted failure evidence after restart/controller rebuild.
// Live prompts and task-local grants are never replayed: old counters become
// historical evidence only and do not re-arm anything.
func (g *Gate) Restore(snap Snapshot) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tasks = map[string]*taskRuntime{}
	g.waiters = map[string]chan resolvePayload{}
	g.taskOf = map[string]string{}
	g.pending = map[string]PendingProposal{}
	g.awaiting = map[string]struct{}{}
	// Fresh Episode after restore/session switch.
	g.episodeSeq++
	if g.episodeSeq == 0 {
		g.episodeSeq = 1
	}
	g.episodeID = fmt.Sprintf("ep:%d", g.episodeSeq)
	g.generation++
	if g.generation == 0 {
		g.generation = 1
	}
	g.metrics.EpisodeRotations++
	for id, st := range snap.Tasks {
		rt := taskRuntimeFromState(st)
		if rt == nil {
			continue
		}
		rt.episodeID = g.episodeID
		// Ignore old Pending and ApprovalID — never restore as authorization.
		g.tasks[id] = rt
	}
}
