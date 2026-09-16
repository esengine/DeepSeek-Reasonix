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

	// reasoningReplayStrongProjection records the provider-visible history cutoff
	// after thinking-400 repair; later messages use normal replay. Its anchor
	// resolves the cutoff after old tool-result messages are removed.
	reasoningReplayStrongProjection       int
	reasoningReplayStrongProjectionAnchor string

	// compactionMu guards projection snapshots/install and the in-memory sidecar
	// generation. Network summarization never runs while this lock is held.
	compactionMu sync.Mutex
	// compactionRunMu singleflights the expensive summary transaction without
	// holding the session lock during network I/O.
	compactionRunMu sync.Mutex
	compaction      compactionProgress
	compactionState CompactionState
	cacheState      string // legacy resume telemetry; never provider-visible

	// path is rebound by preflight when a transcript is bound. Checkpoint state
	// and any unconfirmed commit belong to the current conversation and reset.
	path            string // bound transcript path for projection sidecars
	checkpointState string // none|restored|pending|applied; runtime-only
	// pendingModelContextCommit is an event-log commit that was accepted but
	// whose durability barrier did not complete. The exact payload is retained
	// so the next model boundary can retry idempotently before any provider work.
	pendingModelContextCommit *SessionModelContextCommit

	// todoState is an executor-local mirror populated only after the semantic
	// ToolResult commit succeeds. It never rebuilds from transcript text and is
	// never used as frontend or authorization state.
	todoMu      sync.Mutex
	todoState   []evidence.TodoItem
	todoWritten bool

	// lastPrefixShape records the previous provider request's cacheable prefix
	// so usage events can explain prefix churn on the next request. Carried
	// across a conversation swap; see sessionCarryOver.
	lastPrefixShape     PrefixShape
	haveLastPrefixShape bool

	// perseverationStrikes counts consecutive degenerate-generation aborts with no
	// successfully completed provider response in between. It gates the single
	// nudge-and-retry: the first strike retries, the next one stops for the user.
	perseverationStrikes int
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
	r.reasoningReplayStrongProjection = 0
	r.reasoningReplayStrongProjectionAnchor = ""
	r.compactionMu.Lock()
	r.compactionState = CompactionState{} // lineage change; disk reloaded on Resume
	r.cacheState = CacheStateUnknown
	r.checkpointState = "none"
	r.pendingModelContextCommit = nil
	r.compactionMu.Unlock()
	r.compaction.stuck = false
	r.compaction.stuckInputHash = ""
	r.compaction.consecutive = 0
	r.compaction.failedTurn.Store(0)
	r.compaction.lastTurn.Store(0)
	r.todoMu.Lock()
	r.todoState = nil
	r.todoWritten = false
	r.todoMu.Unlock()
	r.perseverationStrikes = 0
}

// clearReasoningReplayStrongProjection drops the process-local repair overlay.
// The overlay is tied to one canonical history shape; any rewind, branch, or
// other lineage rewrite must not let an old cutoff/anchor govern the new view.
func (r *sessionRuntime) clearReasoningReplayStrongProjection() {
	if r == nil {
		return
	}
	r.reasoningReplayStrongProjection = 0
	r.reasoningReplayStrongProjectionAnchor = ""
}

// session returns the bound conversation under the lock that guards the
// pointer against a concurrent SetSession.
func (r *sessionRuntime) session() *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conversation
}
