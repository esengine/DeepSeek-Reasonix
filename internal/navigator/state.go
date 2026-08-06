package navigator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// StateKind classifies what layer of state a snapshot captured. The navigator
// keeps all three layers because OSWorld 2.0's implicit-state amnesia comes from
// agents collapsing them into one: a recovered file path (implicit) is treated
// as ephemeral (working) and dropped at the next turn.
type StateKind int

const (
	StateKindWorking  StateKind = iota // current turn's active call stack + intermediate results
	StateKindEpisodic                  // recent N turns, sliding window
	StateKindImplicit                  // recovered facts inferred from indirect sources
)

// String returns a human-readable name for the state kind.
func (k StateKind) String() string {
	switch k {
	case StateKindWorking:
		return "working"
	case StateKindEpisodic:
		return "episodic"
	case StateKindImplicit:
		return "implicit"
	default:
		return "unknown"
	}
}

// Fact is one piece of implicit state — the atoms the navigator fights to keep.
// Source names the tool/result/error it was recovered from so a reviewer can
// trace why the agent believes something it was never told directly.
type Fact struct {
	Key    string    // stable identifier (e.g. "path:/etc/hosts", "id:user-42")
	Value  string    // the recovered value
	Source string    // tool name + result slice where it was observed
	Kind   StateKind // which memory layer it belongs to
	At     time.Time
}

// StateSnapshot is a point-in-time capture of everything the navigator knows
// about the world at one step. Snapshots are the nodes of the StateGraph.
type StateSnapshot struct {
	// Step monotonically increases across the whole task; 0 is the initial
	// state before any action.
	Step int
	At   time.Time

	// WorkingState holds the active turn's tool-call stack and intermediate
	// results — the "what am I doing right now" layer.
	WorkingState string

	// ImplicitFacts are the recovered facts accumulated so far. This is the
	// layer OSWorld 2.0 found agents lose; the navigator carries it forward
	// verbatim across every snapshot instead of re-deriving it from history.
	ImplicitFacts []Fact

	// InterfaceHash is a short digest of the perceived UI state at capture
	// time. Empty when no interface sensor is attached. A change between
	// consecutive snapshots signals dynamic-interface drift.
	InterfaceHash string

	// EnvHash is a short digest of the perceived environment (filesystem
	// listing of the working dir, process list summary). Empty when no env
	// sensor is attached. A change signals environment-update drift.
	EnvHash string

	// Action is the action that produced this snapshot from its parent.
	// Empty for the initial snapshot.
	Action string
	// ParentStep is the step this snapshot was reached from; -1 for root.
	ParentStep int
}

// implicitDigest hashes a fact slice into a short stable string so two
// snapshots can be compared for "same recovered facts" without deep-equal.
func implicitDigest(facts []Fact) string {
	if len(facts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(facts))
	for _, f := range facts {
		keys = append(keys, f.Key+"\x00"+f.Value)
	}
	sort.Strings(keys)
	h := sha256.Sum256([]byte(strings.Join(keys, "\x1f")))
	return hex.EncodeToString(h[:6])
}

// Summary returns a human-readable one-line digest of the snapshot for logs.
func (s StateSnapshot) Summary() string {
	return fmt.Sprintf("step=%d action=%q iface=%s env=%s facts=%d(%s)",
		s.Step, truncate(s.Action, 40), s.InterfaceHash, s.EnvHash,
		len(s.ImplicitFacts), implicitDigest(s.ImplicitFacts))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// StateGraph is a directed graph of StateSnapshots: nodes are snapshots, edges
// are actions. It supports branching (a rollback + different action creates a
// sibling branch) so the ClosedLoopEngine can explore alternatives without
// losing the path that failed.
type StateGraph struct {
	mu    sync.RWMutex
	nodes map[int]StateSnapshot // step → snapshot
	edges []stateEdge
}

type stateEdge struct {
	from int
	to   int
	act  string
}

func NewStateGraph() *StateGraph {
	return &StateGraph{nodes: make(map[int]StateSnapshot)}
}

// Add records a snapshot as a node and, if ParentStep >= 0, an edge from its
// parent. Adding a snapshot whose step already exists replaces it — used when
// a branch is re-explored with corrected state.
func (g *StateGraph) Add(s StateSnapshot) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nodes[s.Step] = s
	if s.ParentStep >= 0 {
		g.edges = append(g.edges, stateEdge{from: s.ParentStep, to: s.Step, act: s.Action})
	}
}

// Get returns the snapshot at a step and whether it exists.
func (g *StateGraph) Get(step int) (StateSnapshot, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s, ok := g.nodes[step]
	return s, ok
}

// Children returns the snapshots reachable in one edge from the given step.
func (g *StateGraph) Children(step int) []StateSnapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []StateSnapshot
	for _, e := range g.edges {
		if e.from == step {
			if s, ok := g.nodes[e.to]; ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// LatestStep returns the highest step number recorded, or -1 if empty.
func (g *StateGraph) LatestStep() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	max := -1
	for step := range g.nodes {
		if step > max {
			max = step
		}
	}
	return max
}

// StateHistory is the linear trajectory of snapshots the navigator actually
// followed — the "winning path" through the graph. Rewind pops back to a
// prior step so the ClosedLoopEngine can retry from a known-good state.
type StateHistory struct {
	mu     sync.RWMutex
	path   []StateSnapshot
	maxLen int // 0 = unbounded
}

func NewStateHistory(maxLen int) *StateHistory {
	return &StateHistory{maxLen: maxLen}
}

func (h *StateHistory) Append(s StateSnapshot) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.path = append(h.path, s)
	if h.maxLen > 0 && len(h.path) > h.maxLen {
		// Drop the oldest, but never the root — the initial state is the
		// anchor a rewind targets.
		keep := h.path[1:]
		h.path = append([]StateSnapshot{h.path[0]}, keep[len(keep)-h.maxLen+1:]...)
	}
}

func (h *StateHistory) Rewind(toStep int) (StateSnapshot, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.path) - 1; i >= 0; i-- {
		if h.path[i].Step == toStep {
			h.path = h.path[:i+1]
			return h.path[i], true
		}
	}
	return StateSnapshot{}, false
}

func (h *StateHistory) Latest() (StateSnapshot, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.path) == 0 {
		return StateSnapshot{}, false
	}
	return h.path[len(h.path)-1], true
}

func (h *StateHistory) All() []StateSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]StateSnapshot, len(h.path))
	copy(out, h.path)
	return out
}

// StatePredictor forecasts the expected next snapshot given a history and a
// proposed action. It is intentionally lightweight — a perfect predictor would
// be the agent itself. The value is giving the ClosedLoopEngine something
// concrete to compare against, so "the screen didn't change at all" or "the
// file I expected to appear didn't" becomes a machine-detectable deviation
// rather than a vague feeling.
type StatePredictor struct {
	// ActionEffectMap records, per action verb, which hash fields it tends to
	// change. A "read" action shouldn't touch EnvHash; a "write" should. When
	// the observation violates this expectation, the comparator flags it.
	actionEffectMap map[string]actionEffect
}

type actionEffect struct {
	changesInterface bool
	changesEnv       bool
	producesFact     bool
}

func NewStatePredictor() *StatePredictor {
	return &StatePredictor{actionEffectMap: defaultActionEffects()}
}

func defaultActionEffects() map[string]actionEffect {
	return map[string]actionEffect{
		"read":   {changesInterface: false, changesEnv: false, producesFact: true},
		"write":  {changesInterface: false, changesEnv: true, producesFact: false},
		"exec":   {changesInterface: true, changesEnv: true, producesFact: true},
		"click":  {changesInterface: true, changesEnv: false, producesFact: false},
		"type":   {changesInterface: true, changesEnv: false, producesFact: false},
		"scroll": {changesInterface: true, changesEnv: false, producesFact: false},
		"wait":   {changesInterface: false, changesEnv: false, producesFact: false},
	}
}

// Predict returns the snapshot the navigator expects to see after `action` is
// applied to `from`. The prediction is conservative: it only asserts which
// hash fields should/shouldn't change, not their new values (those come from
// the real observation).
func (p *StatePredictor) Predict(from StateSnapshot, action string) StateSnapshot {
	pred := StateSnapshot{
		Step:          from.Step + 1,
		At:            time.Now(),
		ParentStep:    from.Step,
		Action:        action,
		ImplicitFacts: from.ImplicitFacts, // carry forward — never drop on prediction
		WorkingState:  action,
	}
	eff := p.actionEffectMap[actionVerb(action)]
	// Predicted "did not change" is represented by inheriting the parent hash.
	// The comparator then checks: if the real observation's hash differs on a
	// field the action shouldn't touch, that's drift.
	pred.InterfaceHash = from.InterfaceHash
	pred.EnvHash = from.EnvHash
	if eff.changesInterface {
		pred.InterfaceHash = "" // expect change; specific value unknown
	}
	if eff.changesEnv {
		pred.EnvHash = ""
	}
	return pred
}

func actionVerb(action string) string {
	action = strings.TrimSpace(action)
	if i := strings.IndexByte(action, ' '); i >= 0 {
		return strings.ToLower(action[:i])
	}
	return strings.ToLower(action)
}

// ContinuousStateManager is the navigator's always-on state core. It owns the
// graph, the history, and the accumulated implicit facts, and exposes the
// BeforeAction/AfterAction hooks the kernel calls around every tool call.
//
// Unlike compaction-based state preservation (which only fires when the context
// is near-full and then asks a model to summarize), the manager records state
// on every action — so implicit facts survive even a mid-task crash, because
// they live in the manager's memory, not in the prompt.
type ContinuousStateManager struct {
	graph   *StateGraph
	history *StateHistory
	pred    *StatePredictor

	mu     sync.RWMutex
	facts  []Fact         // accumulated implicit facts, keyed by Key for dedup
	factIx map[string]int // Key → index in facts, for upsert
}

func NewContinuousStateManager(historyWindow int) *ContinuousStateManager {
	return &ContinuousStateManager{
		graph:   NewStateGraph(),
		history: NewStateHistory(historyWindow),
		pred:    NewStatePredictor(),
		factIx:  make(map[string]int),
	}
}

// Seed initializes the root snapshot (step 0) from the initial environment
// observation. Must be called once before BeforeAction.
func (m *ContinuousStateManager) Seed(ctx context.Context, ifaceHash, envHash string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seedLocked(ctx, ifaceHash, envHash)
}

// seedLocked is Seed with the manager lock already held.
func (m *ContinuousStateManager) seedLocked(ctx context.Context, ifaceHash, envHash string) {
	root := StateSnapshot{
		Step:          0,
		At:            time.Now(),
		InterfaceHash: ifaceHash,
		EnvHash:       envHash,
		ParentStep:    -1,
	}
	m.graph.Add(root)
	m.history.Append(root)
}

// BeforeAction returns the predicted next snapshot so the ClosedLoopEngine can
// compare it against the real observation in AfterAction.
func (m *ContinuousStateManager) BeforeAction(ctx context.Context, action string) StateSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.beforeActionLocked(ctx, action)
}

func (m *ContinuousStateManager) beforeActionLocked(ctx context.Context, action string) StateSnapshot {
	prev, ok := m.history.Latest()
	if !ok {
		prev = StateSnapshot{Step: -1, ParentStep: -1}
	}
	return m.pred.Predict(prev, action)
}

// AfterAction records the real observed snapshot, upserts any newly recovered
// implicit facts, and returns the delta vs the prediction (for the engine).
func (m *ContinuousStateManager) AfterAction(ctx context.Context, action string, obs StateSnapshot, recovered []Fact) StateSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.afterActionLocked(ctx, action, obs, recovered)
}

func (m *ContinuousStateManager) afterActionLocked(ctx context.Context, action string, obs StateSnapshot, recovered []Fact) StateSnapshot {
	prev, ok := m.history.Latest()
	if !ok {
		prev = StateSnapshot{Step: -1, ParentStep: -1}
	}
	obs.Step = prev.Step + 1
	obs.ParentStep = prev.Step
	obs.Action = action
	obs.At = time.Now()
	// Carry forward all accumulated implicit facts, then upsert the new ones.
	// This is the core defense against implicit-state amnesia: facts already
	// recovered are never re-derived or dropped — they ride along verbatim.
	obs.ImplicitFacts = m.upsertFactsLocked(append(append([]Fact{}, prev.ImplicitFacts...), recovered...))
	m.graph.Add(obs)
	m.history.Append(obs)
	return obs
}

// upsertFacts merges a fact slice into the manager's accumulated set, deduping
// by Key (later value wins) and returning the merged slice.
func (m *ContinuousStateManager) upsertFacts(incoming []Fact) []Fact {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.upsertFactsLocked(incoming)
}

// upsertFactsLocked is upsertFacts with the manager lock already held.
func (m *ContinuousStateManager) upsertFactsLocked(incoming []Fact) []Fact {
	for _, f := range incoming {
		if f.Key == "" {
			continue
		}
		if idx, ok := m.factIx[f.Key]; ok {
			m.facts[idx] = f // upsert: later observation wins
		} else {
			m.factIx[f.Key] = len(m.facts)
			m.facts = append(m.facts, f)
		}
	}
	out := make([]Fact, len(m.facts))
	copy(out, m.facts)
	return out
}

// ImplicitFacts returns a copy of the accumulated implicit facts — injected
// into compaction summaries so the model never loses them across a fold.
func (m *ContinuousStateManager) ImplicitFacts() []Fact {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Fact, len(m.facts))
	copy(out, m.facts)
	return out
}

// ImplicitStateDigest returns a text rendering of the accumulated implicit
// facts suitable for injection into a compaction summary's "Hidden state &
// recovered facts" section. This is what the StateTracker integration calls.
func (m *ContinuousStateManager) ImplicitStateDigest() string {
	facts := m.ImplicitFacts()
	if len(facts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Recovered %d implicit facts:\n", len(facts)))
	for _, f := range facts {
		b.WriteString(fmt.Sprintf("- %s = %s  (from %s, %s)\n", f.Key, f.Value, f.Source, f.Kind))
	}
	return b.String()
}

// Rewind pops the history back to the given step, so the ClosedLoopEngine can
// retry from a known-good state. The graph keeps all branches for analysis.
func (m *ContinuousStateManager) Rewind(toStep int) (StateSnapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.history.Rewind(toStep)
}

// Graph returns the state graph for analysis/debugging.
func (m *ContinuousStateManager) Graph() *StateGraph {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.graph
}

// History returns the state history for analysis/debugging.
func (m *ContinuousStateManager) History() *StateHistory {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.history
}
