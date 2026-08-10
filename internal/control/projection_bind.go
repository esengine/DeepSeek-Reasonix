package control

import (
	"fmt"
	"log/slog"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

// bindExecutorProjection rebinds the agent's projection sidecar to path.
// loadSidecar=true loads an existing sidecar (resume/switch); false clears
// in-memory projection without deleting another session's .context.json.
func (c *Controller) bindExecutorProjection(path string, loadSidecar bool) {
	if c == nil || c.executor == nil {
		return
	}
	c.executor.BindSessionPath(path, loadSidecar)
}

// maybeColdResumePrune records warm/cold/unknown only; it never rewrites history.
func (c *Controller) maybeColdResumePrune(path string) {
	if c.executor == nil || path == "" {
		return
	}
	// Sidecar path is rebound in Resume; only refresh cache state here.
	m, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok || m.UpdatedAt.IsZero() {
		c.executor.SetCacheState(agent.CacheStateUnknown)
		slog.Info("controller: resume cache state", "path", path, "cache_state", agent.CacheStateUnknown)
		return
	}
	last := m.UpdatedAt
	state := agent.CacheStateWarm
	if time.Since(last) >= c.cacheColdAfter() {
		state = agent.CacheStateCold
	}
	c.executor.SetCacheState(state)
	slog.Info("controller: resume cache state", "path", path, "cache_state", state, "idle", time.Since(last).Round(time.Minute).String())
	if c.disableColdResumePrune || state != agent.CacheStateCold {
		return
	}
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(
		"resumed after %s idle (provider cache likely expired) — full history kept; compaction deferred until context pressure",
		time.Since(last).Round(time.Minute))})
}
