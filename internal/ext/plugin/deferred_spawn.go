// Bookkeeping for servers that connect after the call that asked for them: the
// generation that recognises a stale result, the cancels a Close must reach,
// and the claim that keeps two callers from handshaking with one server.
package plugin

import (
	"context"

	"reasonix/internal/contract/tool"
)

// registerDeferredCancel records a background connect so Close can reach it, and
// returns the generation the caller's result will be checked against. Zero means
// the host is closed and the caller must not start.
func (h *Host) registerDeferredCancel(name string, cancel context.CancelCauseFunc) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		cancel(ErrHostClosed)
		return 0
	}
	if h.deferredCancels == nil {
		h.deferredCancels = make(map[string][]context.CancelCauseFunc)
	}
	if h.deferredGenerations == nil {
		h.deferredGenerations = make(map[string]uint64)
	}
	generation := h.deferredGenerations[name]
	if generation == 0 {
		generation = 1
		h.deferredGenerations[name] = generation
	}
	h.deferredCancels[name] = append(h.deferredCancels[name], cancel)
	return generation
}

// beginDeferredSpawn joins the wait group Close drains, reporting false when the
// host is already closed. The Add happens under h.mu so it cannot race the Wait.
func (h *Host) beginDeferredSpawn() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.deferredWG.Add(1)
	return true
}

func (h *Host) endDeferredSpawn() { h.deferredWG.Done() }

// beginSpawn atomically claims the sole right to spawn the named server.
// Returns owner=true if the caller should proceed. When another caller is
// already spawning the same server, owner=false and done is closed when that
// spawn finishes. A new claim announces its connecting state after releasing
// the lock; a joiner stays quiet.
func (h *Host) beginSpawn(key, server string) (*spawnAttempt, bool) {
	h.spawningMu.Lock()
	if h.spawning == nil {
		h.spawning = make(map[string]*spawnAttempt)
	}
	if attempt, ok := h.spawning[key]; ok {
		h.spawningMu.Unlock()
		return attempt, false
	}
	attempt := &spawnAttempt{server: server, done: make(chan struct{})}
	h.spawning[key] = attempt
	h.spawningMu.Unlock()
	h.announce("%s: connecting", server)
	return attempt, true
}

// endSpawn releases the spawn claim for the named server.
func (h *Host) endSpawn(name string, tools []tool.Tool, err error) {
	h.spawningMu.Lock()
	if attempt, ok := h.spawning[name]; ok {
		attempt.tools = append([]tool.Tool(nil), tools...)
		attempt.err = err
		delete(h.spawning, name)
		close(attempt.done)
	}
	h.spawningMu.Unlock()
}
