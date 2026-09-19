package transcript

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
)

const snapshotCacheBytes = 64 << 20
const snapshotCacheEntries = 3

type frozenSnapshot struct {
	projection *Projection
	bytes      int
}

func validateRecordIdentities(messages []*bufferedMessage) error {
	seen := make(map[string]int, len(messages))
	for index, row := range messages {
		id := row.message.RecordID
		if id == "" {
			return errors.New("transcript snapshot record identity is missing")
		}
		if previous, exists := seen[id]; exists {
			return fmt.Errorf("transcript snapshot record identity %q is duplicated at %d and %d", id, previous, index)
		}
		seen[id] = index
	}
	return nil
}

// freezeLocked retains a bounded number of immutable cuts. Strings already
// owned by settled rows are shared; only a live accumulator is materialized.
// Paging/content therefore continue to describe the requested cut while the
// live projection advances.
func (p *Projection) freezeLocked(id string) (*Projection, error) {
	if id != "" {
		if cached, ok := p.snapshots[id]; ok {
			return cached.projection, nil
		}
		return nil, nil
	}
	id = p.boundaryLocked().SnapshotID
	if cached, ok := p.snapshots[id]; ok {
		return cached.projection, nil
	}
	frozen := &Projection{incarnation: p.incarnation, identity: p.identity, revision: p.revision, covered: p.covered, durable: p.durable,
		runtime: p.runtime, attempts: maps.Clone(p.attempts), prompts: maps.Clone(p.prompts)}
	frozen.buffer.userTurns = p.buffer.userTurns
	used := 0
	for _, row := range p.buffer.messages {
		message := row.materialize()
		used += retainedBytes(reflect.ValueOf(message))
		copy := &bufferedMessage{message: message}
		if message.Role == "assistant" {
			copy.content.replace(message.Content)
			copy.reasoning.replace(message.Reasoning)
		}
		frozen.buffer.messages = append(frozen.buffer.messages, copy)
	}
	// Reject a malformed projection before it becomes a reusable frozen cut.
	// Empty or duplicate identities would alias pagination and content reads.
	if err := validateRecordIdentities(frozen.buffer.messages); err != nil {
		return nil, err
	}
	// The turn index is derived once per cut and shares this cut's lifetime, so
	// paging the body never shrinks navigation and repeated outline reads reuse
	// one pass. Its previews count against the same cache budget.
	frozen.outline = buildOutline(frozen.buffer.messages)
	used += retainedBytes(reflect.ValueOf(frozen.outline))
	runtime, _ := frozen.runtimeLocked()
	used += retainedBytes(reflect.ValueOf(runtime))
	if p.snapshots == nil {
		p.snapshots = make(map[string]frozenSnapshot)
	}
	// The current cut is pinned, including unusually large sessions. Settled
	// body strings are shared with the projection; an oversized current cut
	// evicts every older cut instead of making the conversation unreadable.
	for len(p.snapshotOrder) > 0 && (len(p.snapshotOrder) >= snapshotCacheEntries || p.snapshotBytes+used > snapshotCacheBytes) {
		oldest := p.snapshotOrder[0]
		p.snapshotOrder = p.snapshotOrder[1:]
		p.snapshotBytes -= p.snapshots[oldest].bytes
		delete(p.snapshots, oldest)
	}
	p.snapshots[id] = frozenSnapshot{projection: frozen, bytes: used}
	p.snapshotOrder = append(p.snapshotOrder, id)
	p.snapshotBytes += used
	return frozen, nil
}
