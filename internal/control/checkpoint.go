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
// rewind (file-based) is unaffected. Every store call does its disk I/O off mu —
// mu is taken only to read/swap the store pointer and mutate turn/bound.
type checkpointManager struct {
	// mu guards store, turn, and bound; every critical section under it is short
	// and non-blocking (no disk I/O).
	mu    sync.Mutex
	store *checkpoint.Store
	turn  int
	bound map[int]int
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
func (m *checkpointManager) begin(input string, msgIndex int) {
	m.mu.Lock()
	store := m.store
	if store == nil {
		m.mu.Unlock()
		return
	}
	turn := m.turn
	m.turn++
	m.bound[turn] = msgIndex
	m.mu.Unlock()
	store.Begin(turn, input, msgIndex)
}

// turnsByMessageIndex returns message-log index -> checkpoint turn over live
// boundaries. The desktop transcript uses this authoritative map instead of
// recounting visible user bubbles, which can diverge when synthetic user-role
// messages are hidden from the UI.
func (m *checkpointManager) turnsByMessageIndex() map[int]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int]int, len(m.bound))
	for turn, index := range m.bound {
		if existing, ok := out[index]; ok && existing < turn {
			continue
		}
		out[index] = turn
	}
	return out
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
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	if store == nil {
		return nil
	}
	return store.List()
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
	m.mu.Unlock()
	if store != nil {
		store.TruncateFrom(turn)
	}
}

// clearBounds drops every boundary after a summarize restructures the log (so
// conversation rewind degrades to "unavailable" until fresh turns rebuild them)
// while keeping turn monotonic so new turns don't collide with the store.
func (m *checkpointManager) clearBounds() {
	m.mu.Lock()
	m.bound = map[int]int{}
	m.mu.Unlock()
}
