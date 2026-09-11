package control

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/memory"
)

// memoryManager owns the session's loaded memory snapshot, the queue of pending
// standing-document notes, and the serialization of memory writes — behind its own locks
// and off the controller's c.mu. Like goalMachine it is a strict leaf: its
// methods only touch its own state and never call back into the Controller, so a
// memory-panel save can't stall an approval or status poll on c.mu.
//
// set is an immutable snapshot: reads take mu briefly and return the pointer.
// Writes are serialized by writeMu and do their disk I/O (the doc/store write
// plus the memory.Load re-discovery) OFF mu, taking mu only to swap the freshly
// discovered snapshot in and queue the turn-tail note — so a write never holds a
// lock across a filesystem walk. Standing-document edits queue a compatibility
// note because their authoritative copy remains in system until reload. Background
// fact writes only refresh set; the next real user turn publishes the replacement
// session-context snapshot. All write methods are no-ops returning "" when memory
// is disabled (set == nil).
type memoryManager struct {
	// mu guards set (the snapshot pointer) and pending (the turn-tail queue);
	// every critical section under it is short and non-blocking.
	mu  sync.Mutex
	set *memory.Set
	// pending holds standing-document notes added mid-session (via "#" quick-add
	// or a doc edit). Compose drains them onto the next outgoing turn. Background
	// facts never enter this queue; their live replacement snapshot is injected by
	// the turn-context path.
	pending    []string
	lastRecall memory.RecallResult
	autoWrites map[[32]byte]int

	// writeMu serializes memory writes so each write+reload+swap is atomic with
	// respect to the others. Taken OFF mu, so a read (current/drainPending) never
	// blocks behind a write's disk I/O.
	writeMu sync.Mutex
}

func (m *memoryManager) authorizeAutoRemember(args json.RawMessage) {
	key := sha256.Sum256(args)
	m.mu.Lock()
	if m.autoWrites == nil {
		m.autoWrites = map[[32]byte]int{}
	}
	m.autoWrites[key]++
	m.mu.Unlock()
}

func (m *memoryManager) revokeAutoRemember(args json.RawMessage) {
	key := sha256.Sum256(args)
	m.mu.Lock()
	delete(m.autoWrites, key)
	m.mu.Unlock()
}

func (m *memoryManager) clearAutoRemember() {
	m.mu.Lock()
	m.autoWrites = nil
	m.mu.Unlock()
}

func (m *memoryManager) claimAutoRemember(args json.RawMessage) bool {
	key := sha256.Sum256(args)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.autoWrites[key] <= 0 {
		return false
	}
	if m.autoWrites[key] == 1 {
		delete(m.autoWrites, key)
	} else {
		m.autoWrites[key]--
	}
	return true
}

func (m *memoryManager) recall(query string) memory.RecallResult {
	result := m.current().AutoRecall(query, memory.RecallOptions{})
	m.recordRecall(result)
	return result
}

func (m *memoryManager) recordRecall(result memory.RecallResult) {
	m.mu.Lock()
	m.lastRecall = result
	m.mu.Unlock()
}

func (m *memoryManager) lastRecallResult() memory.RecallResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRecall
}

func newMemoryManager(set *memory.Set) memoryManager {
	return memoryManager{set: set}
}

// memoryRecallAudit strips a recall decision to its content-free fingerprint
// for the trajectory/telemetry channel.
func memoryRecallAudit(result memory.RecallResult) event.MemoryRecallAudit {
	audit := event.MemoryRecallAudit{
		UsedChars: result.UsedChars, Omitted: result.Omitted, Suppressed: result.Suppressed,
	}
	for _, hit := range result.Hits {
		audit.Hits = append(audit.Hits, event.MemoryRecallHit{
			ID: hit.Memory.ID, Revision: hit.Memory.Revision,
			Scope:     string(memory.NormalizeFactScope(string(hit.Memory.Scope))),
			Type:      string(memory.NormalizeType(string(hit.Memory.Type))),
			Freshness: hit.Freshness, Score: hit.Score,
		})
	}
	for _, hit := range result.ShadowHits {
		audit.Shadow = append(audit.Shadow, event.MemoryRecallHit{ID: hit.ID, Score: hit.Score})
	}
	return audit
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

// applyBackgroundWrite refreshes the live background-memory snapshot without
// generating a legacy <memory-update>. The next real user turn observes the new
// BackgroundDataBlock and appends one complete replacement session-context.
func (m *memoryManager) applyBackgroundWrite(mem *memory.Set) {
	m.applyWrite(mem, "")
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
	m.applyBackgroundWrite(mem)
	return path, nil
}

// forget removes a saved auto-memory by name — the panel/TUI forget action, the
// manual counterpart to the model's `forget` tool. The file is archived for
// traceability by Store.Delete; the next real turn publishes the new snapshot.
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
	m.applyBackgroundWrite(mem)
	return nil
}

func (m *memoryManager) revisions(ref string) []memory.Memory {
	mem := m.current()
	if mem == nil {
		return nil
	}
	return mem.Store.Revisions(ref)
}

func (m *memoryManager) restore(ref string, revision int) (memory.Memory, error) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	mem := m.current()
	if mem == nil {
		return memory.Memory{}, fmt.Errorf("memory unavailable")
	}
	result, err := mem.Store.Restore(ref, revision)
	if err != nil {
		return memory.Memory{}, err
	}
	m.applyBackgroundWrite(mem)
	return result.Memory, nil
}

func (m *memoryManager) restoreArchived(archivePath string) (memory.Memory, error) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	mem := m.current()
	if mem == nil {
		return memory.Memory{}, fmt.Errorf("memory unavailable")
	}
	result, err := mem.Store.RestoreArchived(archivePath)
	if err != nil {
		return memory.Memory{}, err
	}
	m.applyBackgroundWrite(mem)
	return result.Memory, nil
}

// queue is the model remember/forget tool callback. The tool result already
// reports the mutation inside the current loop; only the refreshed background
// snapshot is needed for the next real user turn.
func (m *memoryManager) queue(_ string) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	if mem := m.current(); mem != nil {
		m.applyBackgroundWrite(mem)
	}
}
