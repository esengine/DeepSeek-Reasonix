package control

import (
	"fmt"
	"sync"

	"reasonix/internal/checkpoint"
	"reasonix/internal/diff"
)

// checkpointManager owns the snapshot-based rewind bookkeeping: the per-session
// checkpoint store, the monotonic turn counter, and the conversation-rewind
// boundary map. Like approvalManager it holds only the bookkeeping behind its own
// lock, off the controller's c.mu — the Controller keeps the rewind/fork
// orchestration (truncating the session, restoring code, emitting events) that
// needs its other collaborators.
//
// turn is decoupled from the store so it never collides after a log restructure;
// bound[turn] records len(Session.Messages) at that turn's start — the truncation
// boundary for a conversation rewind/fork. Boundaries are persisted in each
// checkpoint and rebuilt from the store on resume (so a reopened session can still
// rewind conversation / fork), but dropped after a summarize restructures the log
// so those operations report "unavailable" rather than mis-truncating; code
// rewind (file-based) is unaffected. Begin/truncate keep the manager lock through
// their matching Store mutation so a consistent capture cannot observe only one
// half of the transition. Independent file snapshot/restore I/O still runs off
// the manager lock and is serialized by Store itself.
type checkpointManager struct {
	// mu guards store, turn, bound, and the atomic visibility boundary described
	// above. The lock order is manager -> Store; Store never calls back here.
	mu    sync.Mutex
	store *checkpoint.Store
	turn  int
	bound map[int]int

	// beforeStoreMutation is a deterministic test seam invoked while mu is
	// held after the manager-side state has changed but before the matching
	// Store mutation. Production leaves it nil. It proves capture cannot expose
	// either half of that otherwise-observable transition.
	beforeStoreMutation func(kind string)
}

// CheckpointSnapshot is one consistent, deeply-owned view of checkpoint
// metadata and conversation capabilities. TurnsByMessageIndex is the mapping
// consumed by transcript projection; ConversationAvailable reports whether
// each displayed checkpoint still has a live message boundary.
type CheckpointSnapshot struct {
	Metas                 []checkpoint.Meta
	TurnsByMessageIndex   map[int]int
	ConversationAvailable map[int]bool
}

// rebind points the store at the (possibly new) session, loading any checkpoints
// already on disk, and resets the turn counter and boundaries from them. root is
// the workspace root used to guard restore writes. Called on construction and
// whenever the session path changes (NewSession/Resume/SetSessionPath/fork).
func (m *checkpointManager) rebind(dir, root string) {
	store := checkpoint.New(dir, root)
	next := store.NextTurn() // continue numbering past any checkpoints on disk
	bound := store.Bounds()  // rebuilt from persisted checkpoints so a resumed
	if bound == nil {        // session can still rewind conversation / fork
		bound = map[int]int{}
	}
	m.mu.Lock()
	m.store = store
	m.turn = next
	m.bound = bound
	m.mu.Unlock()
}

// enabled reports whether a checkpoint store is bound.
func (m *checkpointManager) enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store != nil
}

// begin opens a checkpoint for the turn about to run, recording msgIndex as the
// conversation-rewind boundary. No-op when checkpoints are disabled.
func (m *checkpointManager) begin(input string, msgIndex int) (int, bool) {
	m.mu.Lock()
	store := m.store
	if store == nil {
		m.mu.Unlock()
		return 0, false
	}
	turn := m.turn
	m.turn++
	m.bound[turn] = msgIndex
	if m.beforeStoreMutation != nil {
		m.beforeStoreMutation("begin")
	}
	store.Begin(turn, input, msgIndex)
	m.mu.Unlock()
	return turn, true
}

// turnsByMessageIndex returns message-log index -> checkpoint turn over live
// boundaries. The desktop transcript uses this authoritative map instead of
// recounting visible user bubbles, which can diverge when synthetic user-role
// messages are hidden from the UI.
func (m *checkpointManager) turnsByMessageIndex() map[int]int {
	return m.capture().TurnsByMessageIndex
}

// boundary returns the recorded turn-start message index, if any.
func (m *checkpointManager) boundary(turn int) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.bound[turn]
	return b, ok
}

// list returns the checkpoint metadata (nil when disabled).
func (m *checkpointManager) list() []checkpoint.Meta {
	return m.capture().Metas
}

// capture freezes Store metadata and manager conversation boundaries behind
// the same manager lock used by begin/truncate. Store methods only take the
// Store's own mutex and never call back into checkpointManager, so the lock
// order is one-way (manager -> Store).
func (m *checkpointManager) capture() CheckpointSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		return CheckpointSnapshot{
			// Preserve the legacy Checkpoints() nil-when-disabled contract.
			Metas:                 nil,
			TurnsByMessageIndex:   map[int]int{},
			ConversationAvailable: map[int]bool{},
		}
	}

	metas := m.store.List()
	turnsByMessageIndex := make(map[int]int, len(m.bound))
	for turn, index := range m.bound {
		if existing, ok := turnsByMessageIndex[index]; ok && existing < turn {
			continue
		}
		turnsByMessageIndex[index] = turn
	}
	conversationAvailable := make(map[int]bool, len(metas))
	for _, meta := range metas {
		_, conversationAvailable[meta.Turn] = m.bound[meta.Turn]
	}
	return CheckpointSnapshot{
		Metas:                 metas,
		TurnsByMessageIndex:   turnsByMessageIndex,
		ConversationAvailable: conversationAvailable,
	}
}

// restoreCode reverts every file changed at or after turn to its pre-turn
// content. Errors when checkpoints are disabled.
func (m *checkpointManager) restoreCode(turn int) (written, deleted []string, err error) {
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	if store == nil {
		return nil, nil, fmt.Errorf("checkpoints unavailable")
	}
	return store.RestoreCode(turn)
}

// snapshot records a pre-edit file change into the open checkpoint — the
// executor's pre-edit hook. No-op when disabled.
func (m *checkpointManager) snapshot(ch diff.Change) {
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	if store != nil {
		store.Snapshot(ch)
	}
}

// truncateFrom renumbers future turns from `turn` and drops every boundary at or
// after it — the conversation-rewind renumber after the message log is cut back.
func (m *checkpointManager) truncateFrom(turn int) {
	m.mu.Lock()
	store := m.store
	m.turn = turn
	for k := range m.bound {
		if k >= turn {
			delete(m.bound, k)
		}
	}
	if store != nil {
		if m.beforeStoreMutation != nil {
			m.beforeStoreMutation("truncate")
		}
		store.TruncateFrom(turn)
	}
	m.mu.Unlock()
}

// clearBounds drops every boundary after a summarize restructures the log (so
// conversation rewind degrades to "unavailable" until fresh turns rebuild them)
// while keeping turn monotonic so new turns don't collide with the store.
func (m *checkpointManager) clearBounds() {
	m.mu.Lock()
	m.bound = map[int]int{}
	m.mu.Unlock()
}
