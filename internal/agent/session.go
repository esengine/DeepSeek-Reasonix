// Package agent wires a Provider, a tool Registry, and a Session into the
// harness loop that drives a coding task to completion.
package agent

import (
	"sync"

	"reasonix/internal/provider"
)

// Session holds the conversation history for one task. The run loop (one turn at
// a time) is the only writer, but a frontend can read History/Save from another
// goroutine while a turn appends, so mu guards Messages. Direct Messages reads on
// the run-loop goroutine stay lock-free (serial with its own writes); cross-
// goroutine access goes through Snapshot.
type Session struct {
	mu             sync.RWMutex
	Messages       []provider.Message
	version        uint64
	rewriteVersion int // bumped each time the log is rewritten (compact/fold)
	// persistedRewriteVersion is the highest rewriteVersion whose transcript
	// has fully reached disk. It lives on the Session — not on the controller
	// — so swapping session objects can never orphan or misattribute the
	// baseline: NeedsRewriteSave always compares a session against its own
	// save history. Save paths advance it under s.mu with the rewriteVersion
	// captured alongside the message snapshot, never a re-read one, so a
	// compaction landing mid-save stays unpersisted.
	persistedRewriteVersion int
	persisted               sessionPersistState
	// normalizedDirty is set when LoadSession repaired the history on the way in
	// (empty tool-call names, dangling calls, truncated args, …). The repair
	// already lives in Messages, so the next Save persists it automatically as
	// part of the usual full rewrite; the flag exists for observability and to
	// let callers opt out of work that a dirty session would make redundant.
	normalizedDirty bool
	// eventLogDamaged is set when LoadSession found the on-disk event log torn
	// or corrupt and returned the replayable prefix (or the .jsonl checkpoint).
	// The next save heals the log with a rewrite-and-compact.
	eventLogDamaged bool
	// rawMessages preserves the pre-normalization transcript when the load-time
	// repairs changed it (normalizedDirty). It is only meaningful on a freshly
	// loaded Session: checkSnapshotWrite compares a pending snapshot against
	// what is actually on disk, and the repaired view no longer represents
	// those bytes — a session that kept running extends the raw transcript.
	rawMessages []provider.Message
}

// NewSession initializes a session with an optional system prompt.
func NewSession(system string) *Session {
	s := &Session{}
	if system != "" {
		s.Messages = append(s.Messages, provider.Message{Role: provider.RoleSystem, Content: system})
	}
	return s
}

// Add appends a message.
func (s *Session) Add(m provider.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = append(s.Messages, m)
	s.version++
}

// Replace swaps the whole message log — used by compaction, which rewrites the
// middle of the history.
func (s *Session) Replace(msgs []provider.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = msgs
	s.version++
}

// Snapshot returns a copy of the messages, safe to read from another goroutine
// while a turn appends. Frontends (History, Save) use it instead of touching the
// live slice.
func (s *Session) Snapshot() []provider.Message {
	msgs, _, _ := s.snapshotWithVersion()
	return msgs
}

// Len returns the number of messages, safe to call from any goroutine.
func (s *Session) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Messages)
}

// CloneWithMessages returns a fresh Session carrying msgs while preserving the
// persistence baseline of the source session. Resume paths use this when they
// need to adjust loaded history before a rewrite; dropping persisted would make
// CAS treat the first legitimate rewrite as a stale-runtime conflict.
//
// Callers that are handed history from outside this Session should prefer
// CloneWithMessagesIfCompatible, so stale carried history cannot borrow a newer
// on-disk baseline.
func (s *Session) CloneWithMessages(msgs []provider.Message) *Session {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	version := s.version
	if !messagesEqualForStorageList(s.Messages, msgs) {
		version++
	}
	return &Session{
		Messages:                append([]provider.Message(nil), msgs...),
		version:                 version,
		rewriteVersion:          s.rewriteVersion,
		persistedRewriteVersion: s.persistedRewriteVersion,
		persisted:               s.persisted,
		normalizedDirty:         s.normalizedDirty,
		eventLogDamaged:         s.eventLogDamaged,
	}
}

// CloneWithMessagesIfCompatible preserves the persistence baseline only when
// msgs is the same persisted history, optionally with a refreshed leading system
// prompt. Other history changes must happen after Resume so SaveRewrite can
// still detect genuine stale-controller conflicts.
func (s *Session) CloneWithMessagesIfCompatible(msgs []provider.Message) (*Session, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !messagesCompatibleForStorageBaseline(s.Messages, msgs) {
		return nil, false
	}
	version := s.version
	if !messagesEqualForStorageList(s.Messages, msgs) {
		version++
	}
	return &Session{
		Messages:                append([]provider.Message(nil), msgs...),
		version:                 version,
		rewriteVersion:          s.rewriteVersion,
		persistedRewriteVersion: s.persistedRewriteVersion,
		persisted:               s.persisted,
		normalizedDirty:         s.normalizedDirty,
		eventLogDamaged:         s.eventLogDamaged,
	}, true
}

// snapshotWithVersion returns the messages together with the version and
// rewriteVersion they were captured under, in one lock window: save paths
// persist exactly this rewriteVersion as the new baseline, so a rewrite that
// lands after the capture cannot be misrecorded as saved.
func (s *Session) snapshotWithVersion() ([]provider.Message, uint64, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]provider.Message(nil), s.Messages...), s.version, s.rewriteVersion
}

// RewriteVersion returns the current rewrite version.
func (s *Session) RewriteVersion() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rewriteVersion
}

// NeedsRewriteSave reports whether the history has been rewritten in memory
// (compaction, prune) since the last successful full save of this session.
// Snapshot paths use it to decide that the next write must be an owned
// rewrite instead of an append.
func (s *Session) NeedsRewriteSave() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rewriteVersion > s.persistedRewriteVersion
}

// IncrementRewrite bumps the rewrite version by 1.
func (s *Session) IncrementRewrite() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rewriteVersion++
	s.version++
}

// HasContent returns true when the session carries at least one user,
// assistant, or tool message — i.e. more than just a system prompt. An
// "empty" conversation that has never been used should not be persisted.
func (s *Session) HasContent() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.Messages {
		if m.Role != provider.RoleSystem {
			return true
		}
	}
	return false
}

// HasSystemMessage reports whether the session starts with a system message,
// which carries the agent's stable identity and behavioural contract. Sessions
// without one are not safe to persist: when reloaded the model has no identity
// context and falls back to its training-data defaults.
func (s *Session) HasSystemMessage() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Messages) > 0 && s.Messages[0].Role == provider.RoleSystem
}
