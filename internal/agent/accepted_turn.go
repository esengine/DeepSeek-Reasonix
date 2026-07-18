package agent

import (
	"context"
	"sync"

	"reasonix/internal/provider"
)

// acceptedTurnContextKey carries the crash-safe user-message boundary that a
// Controller established before invoking a Runner. The pointer value is shared
// by Coordinator's planner and executor passes, so only the Agent bound to the
// target Session may consume it.
type acceptedTurnContextKey struct{}

// AcceptedTurnRef identifies one already-appended user message. Its fields are
// intentionally private: Controllers create refs through WithAcceptedTurn and
// Agents consume them through ReuseAcceptedTurn, keeping the exactly-once and
// Session-identity checks in one place.
type AcceptedTurnRef struct {
	mu           sync.Mutex
	session      *Session
	messageIndex int
	consumed     bool
	observers    []func(content string)
}

// WithAcceptedTurn binds an already-appended, durably accepted user message to
// ctx. A Coordinator may pass the same context through its planner and executor:
// a planner with a different Session cannot consume the ref, while the executor
// updates the accepted message exactly once instead of appending a duplicate.
func WithAcceptedTurn(ctx context.Context, session *Session, messageIndex int) context.Context {
	return context.WithValue(ctx, acceptedTurnContextKey{}, &AcceptedTurnRef{
		session:      session,
		messageIndex: messageIndex,
	})
}

// HasAcceptedTurn reports whether ctx belongs to a top-level turn whose user
// message was accepted before Runner execution. It deliberately remains true
// after the executor consumes the ref: Controller-internal synthetic rounds use
// this fact to retain the one top-level marker/checkpoint lifecycle.
func HasAcceptedTurn(ctx context.Context) bool {
	return acceptedTurnRef(ctx) != nil
}

// AcceptedTurnMessageIndex returns the bound Session message index. The index
// remains available after consumption for top-level checkpoint bookkeeping.
func AcceptedTurnMessageIndex(ctx context.Context) (int, bool) {
	ref := acceptedTurnRef(ctx)
	if ref == nil {
		return 0, false
	}
	ref.mu.Lock()
	defer ref.mu.Unlock()
	return ref.messageIndex, true
}

// OnAcceptedTurnReuse registers a synchronous observer for the one Agent reuse
// of ctx's accepted user message. The observer receives the final provider-
// visible content before Session.Content is rewritten, so a display sidecar can
// be persisted before any concurrent autosave can expose the composed prompt.
// It runs without either the ref or Session lock held. false means ctx has no
// accepted ref, the ref was already consumed, or observer is nil.
func OnAcceptedTurnReuse(ctx context.Context, observer func(content string)) bool {
	ref := acceptedTurnRef(ctx)
	if ref == nil || observer == nil {
		return false
	}
	ref.mu.Lock()
	defer ref.mu.Unlock()
	if ref.consumed {
		return false
	}
	ref.observers = append(ref.observers, observer)
	return true
}

// ConsumeAcceptedTurn claims the accepted user message for session without
// rewriting it. Controller-owned execution paths that do not call Agent.Run
// (for example a slash-invoked subagent) use the returned index to update the
// same message themselves. A later Agent.Run on the same top-level context then
// falls back to its ordinary append path, while HasAcceptedTurn remains true.
func ConsumeAcceptedTurn(ctx context.Context, session *Session) (int, bool) {
	ref := acceptedTurnRef(ctx)
	if ref == nil || session == nil {
		return 0, false
	}
	ref.mu.Lock()
	defer ref.mu.Unlock()
	if ref.consumed || ref.session != session || !session.isAcceptedUser(ref.messageIndex) {
		return 0, false
	}
	ref.consumed = true
	// A Controller-only consume does not rewrite the accepted content. Drop
	// reuse observers without firing them; the owning Controller is responsible
	// for any display bookkeeping associated with its direct mutation.
	ref.observers = nil
	return ref.messageIndex, true
}

func acceptedTurnRef(ctx context.Context) *AcceptedTurnRef {
	if ctx == nil {
		return nil
	}
	ref, _ := ctx.Value(acceptedTurnContextKey{}).(*AcceptedTurnRef)
	return ref
}

// ReuseAcceptedTurn replaces the Content and Images of this Agent's accepted
// user message and consumes the ref. Edited and Original are display metadata
// owned by the Controller and remain untouched. false means there was no usable
// ref for this Agent, so callers should retain the ordinary append behavior.
func (a *Agent) ReuseAcceptedTurn(ctx context.Context, content string, images []string) bool {
	if a == nil || a.session == nil {
		return false
	}
	ref := acceptedTurnRef(ctx)
	if ref == nil {
		return false
	}
	ref.mu.Lock()
	if ref.consumed || ref.session != a.session {
		ref.mu.Unlock()
		return false
	}
	// Validate before claiming the one-shot ref. The callback intentionally runs
	// before the mutation and without locks: a display mapping committed first
	// is harmless if the process dies before the rewrite, while the inverse order
	// leaves a crash window where autosave exposes composed prompt internals.
	if !a.session.isAcceptedUser(ref.messageIndex) {
		ref.mu.Unlock()
		return false
	}
	ref.consumed = true
	messageIndex := ref.messageIndex
	observers := append([]func(string){}, ref.observers...)
	ref.observers = nil
	ref.mu.Unlock()
	for _, observer := range observers {
		observer(content)
	}
	if !a.session.rewriteAcceptedUser(messageIndex, content, images) {
		return false
	}
	return true
}

// rewriteAcceptedUser updates only the provider-visible fields of an accepted
// user message. The durable admission snapshot already contains this index, so
// the replacement is an intentional history rewrite and must make the next
// Controller snapshot use SaveRewrite rather than append-only persistence.
func (s *Session) rewriteAcceptedUser(messageIndex int, content string, images []string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if messageIndex < 0 || messageIndex >= len(s.Messages) || s.Messages[messageIndex].Role != provider.RoleUser {
		return false
	}
	s.Messages[messageIndex].Content = content
	s.Messages[messageIndex].Images = append([]string(nil), images...)
	s.version++
	s.rewriteVersion++
	return true
}

func (s *Session) isAcceptedUser(messageIndex int) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return messageIndex >= 0 && messageIndex < len(s.Messages) && s.Messages[messageIndex].Role == provider.RoleUser
}
