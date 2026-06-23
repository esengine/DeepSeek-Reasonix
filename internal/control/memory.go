package control

import (
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/memory"
)

// memoryManager owns the session's loaded memory snapshot, the queue of pending
// turn-tail notes, and the serialization of memory writes — behind its own locks
// and off the controller's c.mu. Like goalMachine it is a strict leaf: its
// methods only touch its own state and never call back into the Controller, so a
// memory-panel save can't stall an approval or status poll on c.mu.
//
// set is an immutable snapshot: reads take mu briefly and return the pointer.
// Writes are serialized by writeMu and do their disk I/O (the doc/store write
// plus the memory.Load re-discovery) OFF mu, taking mu only to swap the freshly
// discovered snapshot in and queue the turn-tail note — so a write never holds a
// lock across a filesystem walk. A turn-tail note is queued for each write so the
// change applies this session without disturbing the cache-stable system prefix
// (it folds into the prefix on the next session). All write methods are no-ops
// returning "" when memory is disabled (set == nil).
type memoryManager struct {
	// mu guards set (the snapshot pointer) and pending (the turn-tail queue);
	// every critical section under it is short and non-blocking.
	mu  sync.Mutex
	set *memory.Set
	// pending holds memory notes added mid-session (via "#" quick-add or a memory
	// edit) that haven't yet been folded into a turn. Compose drains it onto the
	// next outgoing turn — never into the cache-stable system prefix — so a fresh
	// memory takes effect this session without busting the prompt cache; it joins
	// the prefix naturally on the next session.
	pending []string

	// writeMu serializes memory writes so each write+reload+swap is atomic with
	// respect to the others. Taken OFF mu, so a read (current/drainPending) never
	// blocks behind a write's disk I/O.
	writeMu sync.Mutex
}

func newMemoryManager(set *memory.Set) memoryManager {
	return memoryManager{set: set}
}

// current returns the loaded snapshot (nil when memory is disabled). The returned
// *Set is immutable — mutations go through quickAdd / saveDoc / saveMemory.
func (m *memoryManager) current() *memory.Set {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.set
}

// drainPending returns and clears the queued turn-tail notes, for Compose to fold
// onto the next outgoing turn.
func (m *memoryManager) drainPending() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	notes := m.pending
	m.pending = nil
	return notes
}

// applyWrite re-discovers memory from disk (off-lock, the expensive part) then,
// under a brief mu, swaps the fresh snapshot in and queues the turn-tail note so a
// later current() reflects the just-applied write. mem is the snapshot taken at
// the start of the writeMu-serialized write and supplies the discovery roots.
// Callers hold writeMu.
func (m *memoryManager) applyWrite(mem *memory.Set, note string) {
	reloaded := memory.Load(memory.Options{CWD: mem.CWD, UserDir: mem.UserDir})
	m.mu.Lock()
	if note != "" {
		m.pending = append(m.pending, note)
	}
	m.set = reloaded
	m.mu.Unlock()
}

// quickAdd appends a one-line note to the doc-memory file for scope (project
// REASONIX.md by default) — the write side of "#<note>". Returns the file written.
func (m *memoryManager) quickAdd(scope memory.Scope, note string) (string, error) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	mem := m.current()
	if mem == nil {
		return "", nil
	}
	path := mem.DocPath(scope)
	if path == "" {
		return "", fmt.Errorf("no target file for memory scope %q", scope)
	}
	if err := memory.AppendDoc(path, note); err != nil {
		return "", err
	}
	m.applyWrite(mem, note)
	return path, nil
}

// saveDoc overwrites a recognized memory doc with body — the save side of the
// desktop panel's in-place editor. Returns the file written.
func (m *memoryManager) saveDoc(path, body string) (string, error) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	mem := m.current()
	if mem == nil {
		return "", nil
	}
	written, err := mem.WriteDoc(path, body)
	if err != nil {
		return "", err
	}
	// Inject the new content once on the next turn: the cached prefix still holds
	// the pre-edit version this session, so handing the model the current text
	// avoids a stale-guidance gap until the next session re-folds it into the
	// prefix. Trimmed to a single tail note (drained by Compose), not per-turn.
	m.applyWrite(mem,
		"Memory file "+written+" was just edited. Its current contents:\n"+strings.TrimSpace(body))
	return written, nil
}

// saveMemory writes an active auto-memory fact and refreshes the in-session
// snapshot. It is the explicit user-confirmed counterpart to the model-owned
// remember tool, used by management surfaces that preview a candidate first.
func (m *memoryManager) saveMemory(fact memory.Memory) (string, error) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	mem := m.current()
	if mem == nil {
		return "", nil
	}
	path, err := mem.Store.Save(fact)
	if err != nil {
		return "", err
	}
	m.applyWrite(mem,
		"Saved memory \""+fact.Name+"\": "+strings.Join(strings.Fields(fact.Description), " "))
	return path, nil
}

// forget removes a saved auto-memory by name — the panel/TUI forget action, the
// manual counterpart to the model's `forget` tool. It queues a turn-tail note so
// the removal applies this session (the cached prefix still lists the fact until
// the next session re-folds the index). The file is archived for traceability by
// Store.Delete.
func (m *memoryManager) forget(name string) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	mem := m.current()
	if mem == nil {
		return nil
	}
	if err := mem.Store.Delete(name); err != nil {
		return err
	}
	m.applyWrite(mem,
		"Forgot memory \""+name+"\" — disregard its line still shown in the saved-memories index until next session.")
	return nil
}

// queue rides a note on the next turn — the model's remember/forget tool path
// (memory.Queue). It refreshes the snapshot a memory panel reads when memory is
// enabled, and still queues the turn-tail note when it isn't (there's no snapshot
// to re-discover).
func (m *memoryManager) queue(note string) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	if mem := m.current(); mem != nil {
		m.applyWrite(mem, note)
		return
	}
	m.mu.Lock()
	m.pending = append(m.pending, note)
	m.mu.Unlock()
}
