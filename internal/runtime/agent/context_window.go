package agent

import (
	"sync"
	"sync/atomic"

	"reasonix/internal/state/sessionstore"
)

// windowState is what the context window owns for one conversation: the
// installed projection, the compaction in flight, and the latches the
// pressure notices keep. It restarts with the conversation.
type windowState struct {
	// coveredHash remembers the folded prefix's fingerprint under the rewrite
	// counter that makes it valid; gauges read it off the run loop.
	coveredHash atomic.Pointer[coveredHashMemo]
	// compactionMu guards projection snapshots/install and the in-memory sidecar
	// generation. Network summarization never runs while this lock is held.
	compactionMu sync.Mutex
	// compactionRunMu singleflights the expensive summary transaction without
	// holding the session lock during network I/O.
	compactionRunMu sync.Mutex
	compaction      compactionProgress
	compactionState sessionstore.CompactionState
	cacheState      string // legacy resume telemetry; never provider-visible
	// checkpointState is rebound by preflight with path, so reset leaves it
	// to its owner rather than blanking it.
	checkpointState string // none|restored|applied; runtime-only
	// budgetNotice latches which context-pressure rung the model has already
	// been told about. Plain fields: the run loop is the only reader/writer.
	budgetNotice budgetNoticeLatch
}

// reset rebinds the window to a new conversation. checkpointState is left to
// preflight, which rebinds it with the transcript path.
func (w *windowState) reset() {
	// Keyed on a rewrite counter that restarts with the new transcript, so a
	// carried-over entry could answer for history this session never had.
	w.coveredHash.Store(nil)
	w.compactionMu.Lock()
	w.compactionState = sessionstore.CompactionState{} // lineage change; disk reloaded on Resume
	w.cacheState = CacheStateUnknown
	w.compactionMu.Unlock()
	w.compaction.restart()
	w.budgetNotice = budgetNoticeLatch{}
}
