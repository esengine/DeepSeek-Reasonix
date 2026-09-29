package plugin

import "reasonix/internal/contract/tool"

// beginSpawn atomically claims the sole right to spawn the named server.
// A new claim announces its connecting state after releasing the lock.
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
