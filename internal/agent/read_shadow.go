package agent

import (
	"log/slog"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/readcoord"
	"reasonix/internal/tool"
)

// readShadowState retains its rollout name but owns default read execution.
// Legacy comparisons remain diagnostic; read_pipeline consumes its decisions.
type readShadowState struct {
	enabled       bool
	coord         *readcoord.Coordinator
	observed      int
	disagreements int
	byState       map[readcoord.State]int
	// pivots holds the reads whose strategy-change advice is still owed. A
	// single slot would let one stalled read overwrite another's, and the
	// coordinator only offers each read one pivot.
	pivots     map[string]struct{}
	hinted     map[string]uint64
	strategies map[string]bool
	strategyMu *sync.Mutex
}

var readShadowStrategyInitMu sync.Mutex

func (s *readShadowState) lockStrategy() bool {
	if s == nil {
		return false
	}
	if s.strategyMu == nil {
		readShadowStrategyInitMu.Lock()
		if s.strategyMu == nil {
			s.strategyMu = &sync.Mutex{}
		}
		readShadowStrategyInitMu.Unlock()
	}
	s.strategyMu.Lock()
	return true
}

func newReadShadowState(enabled bool) readShadowState {
	s := readShadowState{enabled: enabled, strategyMu: &sync.Mutex{}}
	if enabled {
		s.coord = readcoord.New()
		s.byState = map[readcoord.State]int{}
		s.pivots = map[string]struct{}{}
		s.strategies = map[string]bool{}
	}
	return s
}

// observeReadShadow commits one delivered envelope in provider order. The
// execution loop and status projections consume the same coordinator verdict.
func (a *Agent) observeReadShadow(env tool.ReadResultEnvelope, elapsed ...int64) {
	if a == nil {
		return
	}
	s := &a.turn.readShadow
	if !s.enabled || s.coord == nil {
		return
	}
	var active int64
	if len(elapsed) > 0 {
		active = elapsed[0]
	}
	tr, ok := s.coord.Observe(env, active)
	if !ok {
		return
	}
	// A read that still needs content is only allowed to continue while the
	// host can bound it: an unknown context window or an exhausted budget turns
	// the obligation into needs_scope instead of guessing how much is safe.
	if tr.To == readcoord.StateNeedsMore {
		if env.Source.Snapshot == "" && a.readPipelineActive() {
			tr, _ = s.coord.Narrow(env.ReadID, readcoord.Block{Code: "unversioned_source", Detail: "the bounded reader cannot establish one source version", Recovery: "inspect explicit ranges; a full-file review is not yet proven"})
		}
		if block, bounded := a.readBudgetStop(); bounded {
			if narrowed, ok := s.coord.Narrow(env.ReadID, block); ok {
				tr = narrowed
			}
		}
	}
	if tr.Advice == readcoord.AdvicePivot {
		s.pivots[tr.Key] = struct{}{}
	}
	if tr.Stop != nil && tr.To == readcoord.StateNeedsScope && (tr.Stop.Code == "unknown_window" || tr.Stop.Code == "no_headroom") {
		a.armDefaultReadStrategy(tr.Key, env)
	}
	if a.readPipelineActive() && tr.To == readcoord.StateNeedsMore {
		a.issueReadContinuation(tr, env)
	}
	s.observed++
	s.byState[tr.To]++
	a.emitReadStatus(tr, env)
	outstanding := tr.To == readcoord.StateNeedsMore || tr.To == readcoord.StateNeedsScope || tr.To == readcoord.StateBlocked
	if legacy := a.turn.incompleteReads.hasPending(); legacy != outstanding {
		s.disagreements++
		slog.Debug("agent: read coordinator shadow disagreement",
			"read_id", env.ReadID, "path", env.Source.CanonicalPath,
			"coordinator", tr.To.String(), "legacy_pending", legacy)
	}
}

// The coordinator owns the full-read obligation; the legacy strategy state
// owns only the validated targeted-search receipt. Keeping this bridge here
// makes the budget fallback available to the default pipeline too.
func (a *Agent) armDefaultReadStrategy(key string, env tool.ReadResultEnvelope) {
	if a == nil || key == "" {
		return
	}
	a.turn.readShadow.markStrategy(key, false)
	s := &a.turn.incompleteReads
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureEntriesLocked()
	if _, ok := s.entries[key]; ok {
		return
	}
	entry := &incompleteRead{key: key, readID: key, path: env.Source.CanonicalPath, requestPath: env.Source.CanonicalPath, phase: incompleteReadStrategy, fullRead: true, explicitFull: true, searches: map[string]incompleteReadSearch{}, reads: map[string]incompleteReadWindow{}, strategyVersion: snapshotIncompleteReadFile(env.Source.CanonicalPath)}
	if reader, ok := a.svc.tools.Get("read_file"); ok {
		entry.readTool = reader
	}
	s.addEntryLocked(entry)
}

func (s *readShadowState) strategyPending(key string) bool {
	if !s.lockStrategy() {
		return false
	}
	defer s.strategyMu.Unlock()
	if s.strategies == nil {
		return false
	}
	pending, ok := s.strategies[key]
	return ok && !pending
}

func (s *readShadowState) markStrategy(key string, resolved bool) {
	if !s.lockStrategy() {
		return
	}
	defer s.strategyMu.Unlock()
	if s.strategies == nil {
		s.strategies = map[string]bool{}
	}
	s.strategies[key] = resolved
}

func (s *readShadowState) strategyKeys() []string {
	if s.strategyMu == nil {
		s.strategyMu = &sync.Mutex{}
	}
	s.strategyMu.Lock()
	defer s.strategyMu.Unlock()
	keys := make([]string, 0, len(s.strategies))
	for key := range s.strategies {
		keys = append(keys, key)
	}
	return keys
}

// readBudgetStop reports why automatic continuation must stop, using the same
// live context budget the legacy recovery path already computes.
func (a *Agent) readBudgetStop() (readcoord.Block, bool) {
	budget := a.readAutoRecoveryBudgetFor()
	switch {
	case !budget.known:
		return readcoord.Block{
			Code:     "unknown_window",
			Detail:   "the model's context window is unknown, so the host cannot size a safe automatic read",
			Recovery: "read a narrower window explicitly, or work on an independent item",
		}, true
	case budget.maxTokens <= 0:
		return readcoord.Block{
			Code:     "no_headroom",
			Detail:   "the remaining context cannot hold another automatic page",
			Recovery: "read a narrower window explicitly, or work on an independent item",
		}, true
	}
	return readcoord.Block{}, false
}

// emitReadStatus publishes one logical read's current delivery state. The event
// is keyed by read id and generation, so a hundred pages still update a single
// status rather than appending a hundred notices.
func (a *Agent) emitReadStatus(tr readcoord.Transition, env tool.ReadResultEnvelope) {
	if a == nil || a.svc.sink == nil {
		return
	}
	payload := &event.ReadStatusPayload{
		ReadID:     tr.Key,
		Generation: tr.Generation,
		Sequence:   tr.Sequence,
		Path:       tr.Scope.CanonicalPath,
		Intent:     string(env.Intent),
		State:      tr.To.String(),
		Covered:    linePairs(tr.Covered),
		Missing:    linePairs(tr.Missing),
		SourceEnd:  tr.SourceEnd,
		HasMore:    tr.To == readcoord.StateNeedsMore || tr.To == readcoord.StateNeedsScope || tr.To == readcoord.StateBlocked,
		Active:     !tr.To.Terminal(),
	}
	if tr.Stop != nil {
		payload.Reason = tr.Stop.Code
		payload.Recovery = tr.Stop.Recovery
		payload.Verdict = "read_hard_stop"
	} else if env.Intent == tool.ReadIntentFull && !tr.To.Terminal() {
		payload.Verdict = "full_read_pending"
	} else if env.HasMore && env.Intent != tool.ReadIntentFull {
		payload.Verdict = "partial_read_sufficient"
	}
	a.svc.sink.Emit(event.Event{Kind: event.ReadStatus, ReadStatus: payload})
}

// linePairs preserves zero-based half-open coordinates on the host wire.
// CLI and desktop convert to one-based display exactly once.
func linePairs(ranges []tool.ReadRange) [][2]int {
	if len(ranges) == 0 {
		return nil
	}
	out := make([][2]int, 0, len(ranges))
	for _, r := range ranges {
		if r.Empty() {
			continue
		}
		out = append(out, [2]int{r.Start, r.End})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
