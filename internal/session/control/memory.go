package control

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/contract/event"
	"reasonix/internal/state/memory"
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
	pending    []string
	lastRecall memory.RecallResult
	// delivered is the facts the last turn handed the model as memory context —
	// its recall hits, plus the facts a turn-tail note carried — with the file
	// stamp each had then. The next recall re-stats them; a moved stamp reloads.
	delivered  []deliveredFact
	autoWrites map[[32]byte]int

	// instructions is what the model has of the standing-instruction block. It
	// rides the turn rather than the prefix: the project's own rules are what
	// made the prefix diverge per project, and they need same-session freshness.
	instructions projectionDebt

	// writeMu serializes memory writes so each write+reload+swap is atomic with
	// respect to the others. Taken OFF mu, so a read (current/drainPending) never
	// blocks behind a write's disk I/O.
	writeMu sync.Mutex
}

// instructionsClearedBlock is what replaces the standing instructions when they
// are gone. Silence would leave the rules the model already has standing as
// current, which is the failure a removal is most likely to cause.
const instructionsClearedBlock = "# Instructions\n\nThis project's standing instructions have been removed. Anything you were told under this heading earlier no longer applies."

// owedInstructions returns the standing-instruction block when the model does
// not already have the current one. It rediscovers from disk each turn rather
// than reading the snapshot a write left behind: an edit made past this process
// is still an edit, and nothing announces one.
func (m *memoryManager) owedInstructions(unverified bool) string {
	opts := m.current().LoadOptions()
	if strings.TrimSpace(opts.CWD) == "" {
		// No workspace was resolved for this session. Discovering from the
		// process directory would answer for whatever the host happens to be
		// sitting in, which is not this session's project.
		return ""
	}
	block := memory.InstructionsBlockFor(opts, unverified)
	if block == "" && m.instructions.visible(projectInstructionsTag) {
		block = instructionsClearedBlock
	}
	return m.instructions.owed(projectInstructionsTag, block)
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
	m.refreshDelivered()
	result := m.current().AutoRecall(query, memory.RecallOptions{})
	m.recordRecall(result)
	m.recordDelivered(result)
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
		CharBudget: result.CharBudget, UsedChars: result.UsedChars,
		Omitted:         result.OmittedByLimit + result.OmittedByBudget,
		OmittedByLimit:  result.OmittedByLimit,
		OmittedByBudget: result.OmittedByBudget,
		Suppressed:      result.Suppressed,
		SuppressionCode: string(result.SuppressionCode),
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
// under a brief mu, swaps the fresh snapshot in, queues the turn-tail note so a
// later current() reflects the just-applied write, and baselines the facts that
// note handed the model. Callers hold writeMu.
func (m *memoryManager) applyWrite(mem *memory.Set, note string, delivered ...memory.Memory) {
	reloaded := memory.Load(mem.LoadOptions())
	held := make([]deliveredFact, 0, len(delivered))
	for _, fact := range delivered {
		held = append(held, stampFact(reloaded.Store, fact))
	}
	m.mu.Lock()
	kept := m.delivered
	m.mu.Unlock()
	// The reload settles the disk, so re-stamp what the model holds against the
	// snapshot just loaded and drop what is gone: a stale stamp would reload the
	// whole store again on every later recall. See restampFact.
	settled := make([]deliveredFact, 0, len(kept)+len(held))
	for _, was := range kept {
		if now, ok := restampFact(reloaded.Store, was); ok {
			settled = append(settled, now)
		}
	}
	m.mu.Lock()
	if note != "" {
		m.pending = append(m.pending, note)
	}
	m.set = reloaded
	m.delivered = appendDelivered(settled, held)
	m.mu.Unlock()
}

// restampFact re-reads one fact the model still holds from the store a reload
// just built, and stamps it there. ok is false when the fact is gone from the
// store, so the caller drops it.
func restampFact(store memory.Store, held deliveredFact) (deliveredFact, bool) {
	now, ok, err := store.Read(held.fact.ID)
	if err != nil || !ok {
		return deliveredFact{}, false
	}
	return stampFact(store, now), true
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
	result, err := mem.Store.SaveWithOptions(fact, memory.SaveOptions{})
	if err != nil {
		return "", err
	}
	m.applyWrite(mem,
		"Saved memory \""+fact.Name+"\": "+strings.Join(strings.Fields(fact.Description), " ")+"\n"+strings.TrimSpace(fact.Body),
		result.Memory)
	return result.Path, nil
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
		"Forgot memory \""+name+"\" — disregard its loaded guidance and background-index entry for the rest of this session.")
	return nil
}

func (m *memoryManager) revisions(ref string) ([]memory.Memory, error) {
	mem := m.current()
	if mem == nil {
		return nil, nil
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
	m.applyWrite(mem, fmt.Sprintf("Restored memory %q as revision %d: %s\n%s",
		result.Memory.Name, result.Memory.Revision, strings.Join(strings.Fields(result.Memory.Description), " "), strings.TrimSpace(result.Memory.Body)), result.Memory)
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
	m.applyWrite(mem, fmt.Sprintf("Recovered archived memory %q as revision %d: %s\n%s",
		result.Memory.Name, result.Memory.Revision, strings.Join(strings.Fields(result.Memory.Description), " "), strings.TrimSpace(result.Memory.Body)), result.Memory)
	return result.Memory, nil
}

// queue refreshes the snapshot after the model's own remember/forget tool wrote
// (memory.Queue) and records the typed facts it wrote as delivered, so the next
// recall re-checks their own files. It queues no turn-tail note: the tool result
// is already in the conversation, and a note would say the same thing twice and
// suppress the retrieval that says it better.
func (m *memoryManager) queue(_ string, delivered ...memory.Memory) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	if mem := m.current(); mem != nil {
		m.applyWrite(mem, "", delivered...)
	}
}
