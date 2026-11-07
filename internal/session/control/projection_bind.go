package control

import (
	"fmt"
	"log/slog"
	"reasonix/internal/state/sessionstore"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
)

// bindExecutorProjection rebinds the agent's projection sidecar to path.
// loadSidecar=true loads an existing sidecar (resume/switch); false clears
// in-memory projection without deleting another session's .context.json.
func (c *Controller) bindExecutorProjection(path string, loadSidecar bool) {
	if c == nil || c.executor == nil {
		return
	}
	c.executor.BindSessionPath(path, loadSidecar)
	if loadSidecar {
		c.announceUnusedFold()
	}
}

// announceUnusedFold says why a fold saved beside the session was not reused: the
// full history goes to the model instead, and only the host knows which reason.
func (c *Controller) announceUnusedFold() {
	if c.sink == nil {
		return
	}
	if code, text, ok := agent.FoldNotReusedNotice(c.executor.ProjectionDropped()); ok {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Code: code, Text: text})
	}
}

// coldResumeCompactFloor is the resent prompt below which a cold resume offers no
// fold: the summary a fold writes is a fixed cost a shorter prompt cannot repay.
// It is a product threshold, not a measured break-even.
const coldResumeCompactFloor = 60_000

// maybeColdResumePrune records warm/cold/unknown only; it never rewrites
// history. announce=false keeps the state and drops the notice, for a rebuild
// that re-binds the session in place.
func (c *Controller) maybeColdResumePrune(path string, announce bool) {
	if c.executor == nil || path == "" {
		return
	}
	// Sidecar path is rebound in Resume; only refresh cache state here.
	m, ok, err := sessionstore.LoadBranchMeta(path)
	if err != nil || !ok || m.UpdatedAt.IsZero() {
		c.executor.SetCacheState(agent.CacheStateUnknown)
		slog.Debug("controller: resume cache state", "path", path, "cache_state", agent.CacheStateUnknown)
		return
	}
	idle := time.Since(m.UpdatedAt).Round(time.Minute)
	state := agent.CacheStateWarm
	if time.Since(m.UpdatedAt) >= c.cacheColdAfter() {
		state = agent.CacheStateCold
	}
	c.executor.SetCacheState(state)
	slog.Debug("controller: resume cache state", "path", path, "cache_state", state, "idle", idle.String())
	if !announce || state != agent.CacheStateCold {
		return
	}
	c.sink.Emit(coldResumeNotice(idle, c.executor.ContextUsedTokens(), c.executor.ContextWindow()))
}

// coldResumeNotice offers a fold only past coldResumeCompactFloor. tokens is what
// the next request sends, so a reused fold already shrank it; nothing is folded
// unless the person asks.
func coldResumeNotice(idle time.Duration, tokens, window int) event.Event {
	p := event.ColdResume{IdleSeconds: max(int(idle/time.Second), 60), Tokens: tokens, Window: window, Compact: tokens >= coldResumeCompactFloor}
	text := fmt.Sprintf("resumed after %s idle (provider cache likely expired) — full history kept; compaction deferred until context pressure", idle)
	if p.Compact {
		text = fmt.Sprintf("resumed after %s idle (provider cache likely expired) — the next request resends ~%d tokens uncached; /compact folds them first, at no cache cost", idle, tokens)
	}
	return event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeColdResume, Text: text, Detail: p.Encode()}
}
