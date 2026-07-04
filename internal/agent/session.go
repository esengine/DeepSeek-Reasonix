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
	persisted      sessionPersistState
	// normalizedDirty is set when LoadSession repaired the history on the way in
	// (empty tool-call names, dangling calls, truncated args, …). The repair
	// already lives in Messages, so the next Save persists it automatically as
	// part of the usual full rewrite; the flag exists for observability and to
	// let callers opt out of work that a dirty session would make redundant.
	normalizedDirty bool
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
	msgs, _ := s.snapshotWithVersion()
	return msgs
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
		Messages:        append([]provider.Message(nil), msgs...),
		version:         version,
		rewriteVersion:  s.rewriteVersion,
		persisted:       s.persisted,
		normalizedDirty: s.normalizedDirty,
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
		Messages:        append([]provider.Message(nil), msgs...),
		version:         version,
		rewriteVersion:  s.rewriteVersion,
		persisted:       s.persisted,
		normalizedDirty: s.normalizedDirty,
	}, true
}

func (s *Session) snapshotWithVersion() ([]provider.Message, uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]provider.Message(nil), s.Messages...), s.version
}

// RewriteVersion returns the current rewrite version.
func (s *Session) RewriteVersion() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rewriteVersion
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
