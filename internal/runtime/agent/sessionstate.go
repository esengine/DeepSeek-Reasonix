package agent

import (
	"reasonix/internal/state/sessionstore"
	"sync"
	"sync/atomic"

	"reasonix/internal/contract/provider"
	"reasonix/internal/safety/evidence"
)

// sessionRuntime is the host state one conversation owns. Its lifetime sits
// between the process and the task: SetSession replaces the conversation and
// reset restarts everything here that belongs to it. Atomics and mutexes make
// the whole-value assignment taskRuntime uses illegal, so the "no field is
// forgotten" property is enforced by sessionstate_test.go instead.
type sessionRuntime struct {
	mu           sync.Mutex // guards conversation for external Session()/SetSession
	conversation *sessionstore.Session
	output       outputBudgetState
	// win is the context window's own state; only context-window files touch it.
	win windowState

	// cacheHit/cacheMiss are the session aggregate, which compaction must not
	// reset — the hit-rate would crater every time the visible prefix is folded.
	// Atomic: the run loop accumulates while the status line reads.
	cacheHit  atomic.Int64
	cacheMiss atomic.Int64

	missingReasoning missingReasoningWatch
	// mode is the model mode the conversation asked for; a new one starts off.
	mode requestModeState

	// path is rebound by preflight when a transcript is bound, so reset leaves
	// it to its owner rather than blanking it.
	path string // bound transcript path for projection sidecars

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
	// lastProviderSchemas is the tool surface the last request carried, so an
	// estimate reports what was sent rather than what the registry holds.
	lastProviderSchemas []provider.ToolSchema
}

// reset rebinds the runtime to a new conversation. Every field is named here or
// in sessionCarryOver, and sessionstate_test.go checks both lists against the
// struct: an atomic-bearing type cannot be replaced by one assignment, so the
// guarantee has to be tested rather than compiled.
func (r *sessionRuntime) reset(s *sessionstore.Session) {
	r.mu.Lock()
	r.conversation = s
	r.mu.Unlock()
	r.cacheHit.Store(0)
	r.cacheMiss.Store(0)
	r.output.reset()
	r.missingReasoning = missingReasoningWatch{}
	r.mode.reset()
	r.win.reset()
}

// session returns the bound conversation under the lock that guards the
// pointer against a concurrent SetSession.
func (r *sessionRuntime) session() *sessionstore.Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conversation
}
