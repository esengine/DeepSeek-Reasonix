package agent

import (
	"sync"
	"sync/atomic"

	"reasonix/internal/evidence"
)

// sessionRuntime is the host state one conversation owns. Its lifetime sits
// between the process and the task: SetSession replaces the conversation and
// reset restarts everything here that belongs to it. Atomics and mutexes make
// the whole-value assignment taskRuntime uses illegal, so the "no field is
// forgotten" property is enforced by sessionstate_test.go instead.
type sessionRuntime struct {
	mu           sync.Mutex // guards conversation for external Session()/SetSession
	conversation *Session
	output       outputBudgetState

	// cacheHit/cacheMiss are the session aggregate, which compaction must not
	// reset — the hit-rate would crater every time the visible prefix is folded.
	// Atomic: the run loop accumulates while the status line reads.
	cacheHit  atomic.Int64
	cacheMiss atomic.Int64

	missingReasoning missingReasoningWatch

	// compactionMu guards projection snapshots/install and the in-memory sidecar
	// generation. Network summarization never runs while this lock is held.
	compactionMu sync.Mutex
	// compactionRunMu singleflights the expensive summary transaction without
	// holding the session lock during network I/O.
	compactionRunMu sync.Mutex
	compaction      compactionProgress
	compactionState CompactionState
	cacheState      string // legacy resume telemetry; never provider-visible

	// path and checkpointState are rebound by preflight when a transcript is
	// bound, so reset leaves them to their owner rather than blanking them.
	path            string // bound transcript path for projection sidecars
	checkpointState string // none|restored|applied; runtime-only

	// todoState is the host's canonical task list. It never rides in the prompt,
	// so it survives compaction, and SetSession rebuilds it from the incoming
	// snapshot rather than letting reset blank it.
	todoMu    sync.Mutex
	todoState []evidence.TodoItem

	// lastPrefixShape records the previous provider request's cacheable prefix
	// so usage events can explain prefix churn on the next request. Carried
	// across a conversation swap; see sessionCarryOver.
	lastPrefixShape     PrefixShape
	haveLastPrefixShape bool
}

// reset rebinds the runtime to a new conversation. Every field is named here or
// in sessionCarryOver, and sessionstate_test.go checks both lists against the
// struct: an atomic-bearing type cannot be replaced by one assignment, so the
// guarantee has to be tested rather than compiled.
func (r *sessionRuntime) reset(s *Session) {
	r.mu.Lock()
	r.conversation = s
	r.mu.Unlock()
	r.cacheHit.Store(0)
	r.cacheMiss.Store(0)
	r.output.reset()
	r.missingReasoning = missingReasoningWatch{}
	r.compactionMu.Lock()
	r.compactionState = CompactionState{} // lineage change; disk reloaded on Resume
	r.cacheState = CacheStateUnknown
	r.compactionMu.Unlock()
	r.compaction.stuck = false
	r.compaction.consecutive = 0
	r.compaction.lastTurn.Store(0)
}

// session returns the bound conversation under the lock that guards the
// pointer against a concurrent SetSession.
func (r *sessionRuntime) session() *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conversation
}
