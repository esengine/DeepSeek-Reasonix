package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// ContextManager is the sole owner of provider-visible context maintenance.
// Canonical session messages are immutable inputs; Prepare evolves only the
// durable projection and returns the exact visible view for one sampling round.
type ContextManager struct {
	agent *Agent
}

// ContextPreparePolicy describes one maintenance transaction.
type ContextPreparePolicy struct {
	Trigger      string
	Instructions string
	Force        bool
	// ObservedInputTokens is used by compatibility harnesses that invoke the
	// old post-turn shim directly. Production Prepare estimates the current view
	// from its calibrated final request shape.
	ObservedInputTokens int
}

// PreparedContext is the frozen result of a successful Prepare transaction.
type PreparedContext struct {
	Messages          []provider.Message
	InputTokens       int
	ProjectionVersion uint64
}

func (a *Agent) contextManager() ContextManager { return ContextManager{agent: a} }

// ObserveUsage records provider observations only. It cannot mutate the
// projection; native applied-edit receipts are observational sidecar metadata,
// while any local cleanup they motivate happens in the next request's Prepare.
func (m ContextManager) ObserveUsage(u *provider.Usage) {
	// Sampling recovery already stores and calibrates the latest single-request
	// usage before the run loop reaches this observer. Do not overwrite it with
	// a multi-attempt billable aggregate here.
	if m.agent != nil {
		m.agent.observeNativeContextEditing(u)
	}
}

// Prepare converges the current visible view before sampling. Automatic stale
// compare failures are planned once more; all other failed fingerprints are
// durably blocked until transcript, model, provider policy, or projection
// lineage changes.
func (m ContextManager) Prepare(ctx context.Context, policy ContextPreparePolicy) (PreparedContext, error) {
	if policy.Trigger == "" {
		policy.Trigger = CompactionTriggerPressure
	}
	for range 2 {
		prepared, retry, err := m.prepareOnce(ctx, policy)
		if !retry {
			return prepared, err
		}
	}
	return PreparedContext{}, errCompressStaleContext
}

func (m ContextManager) prepareOnce(ctx context.Context, policy ContextPreparePolicy) (PreparedContext, bool, error) {
	a := m.agent
	if a == nil || a.session == nil {
		return PreparedContext{}, false, nil
	}
	visible := a.modelVisibleMessages()
	prepared := PreparedContext{
		Messages:          append([]provider.Message(nil), visible...),
		InputTokens:       a.estimatedPromptTokens(visible),
		ProjectionVersion: a.currentProjectionVersion(),
	}
	if a.contextWindow <= 0 || len(visible) == 0 {
		return prepared, false, nil
	}
	soft, snip, fold := a.compactThresholds()
	force := a.forceCompactThreshold(fold)
	est := prepared.InputTokens
	if policy.ObservedInputTokens > 0 {
		est = policy.ObservedInputTokens
		prepared.InputTokens = est
	}
	inputHash := a.contextMaintenanceInputHash(visible)
	if blocked, reason := a.contextMaintenanceBlocked(inputHash); blocked {
		if policy.Trigger == CompactionTriggerManual {
			return PreparedContext{}, false, fmt.Errorf("context maintenance is blocked for the current view: %s", reason)
		}
		if policy.Trigger == CompactionTriggerOverflow || (sharesContextWindow(a.prov) && est >= a.hardInputCeiling()) {
			return PreparedContext{}, false, fmt.Errorf("%w: %s", ErrCompactionRequired, reason)
		}
		return prepared, false, nil
	}
	if est < fold {
		a.consecutiveCompacts = 0
		a.compactStuck = false
	}
	if a.compactStuck && policy.Trigger == CompactionTriggerPressure {
		return prepared, false, nil
	}
	if est >= soft && est < snip && !a.softCompactNoticed {
		a.softCompactNoticed = true
		a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Text:   "Context is getting large; preserving cache until cleanup is needed.",
			Detail: fmt.Sprintf("context reached %.0f%% of window; keeping cache-first prefix until compact threshold %.0f%%", a.softCompactRatio*100, a.compactRatio*100)})
	}
	if est < snip && !policy.Force && policy.Trigger != CompactionTriggerManual && policy.Trigger != CompactionTriggerOverflow {
		return prepared, false, nil
	}

	// Native Anthropic tool clearing replaces only local snip/prune. Full-fold
	// summary remains the hard/manual convergence path.
	forceFold := policy.Force || policy.Trigger == CompactionTriggerManual || policy.Trigger == CompactionTriggerOverflow || est >= force
	if maintained, handled, retry, err := m.tryToolMaintenance(visible, prepared, policy, est, fold, forceFold, inputHash); handled {
		return maintained, retry, err
	}
	if est < fold && !forceFold {
		return prepared, false, nil
	}
	return m.foldContext(ctx, prepared, policy, inputHash, est, fold, force, forceFold)
}

func (m ContextManager) tryToolMaintenance(visible []provider.Message, prepared PreparedContext, policy ContextPreparePolicy, est, fold int, forceFold bool, inputHash string) (PreparedContext, bool, bool, error) {
	a := m.agent
	if a.effectiveContextEditing() == "native" {
		return PreparedContext{}, false, false, nil
	}
	mode := toolResultSnip
	if est >= fold || forceFold {
		mode = toolResultPrune
	}
	maintained, stats := a.applyToolResultMaintenanceView(visible, mode)
	if stats.Results == 0 {
		return PreparedContext{}, false, false, nil
	}
	maintainedTokens := a.estimatedPromptTokens(maintained)
	if policy.ObservedInputTokens > 0 {
		maintainedTokens = max(0, est-int(float64(stats.SavedChars)*a.tokPerChar()))
	}
	// An over-trigger result is only an intermediate plan; summary installs the
	// sole prefix switch for this transaction.
	if maintainedTokens >= fold || forceFold || policy.Trigger == CompactionTriggerManual {
		return PreparedContext{}, false, false, nil
	}
	before := a.currentProjectionVersion()
	if err := a.installPruneProjection(maintained, stats); err != nil {
		if errors.Is(err, errCompressStaleContext) || err.Error() == "context maintenance became stale" {
			return PreparedContext{}, true, true, nil
		}
		a.recordContextMaintenanceBlocked(inputHash, policy.Trigger, "noop", fmt.Sprintf("tool-result maintenance failed: %v", err))
		return prepared, true, false, nil
	}
	if a.currentProjectionVersion() > before {
		return m.currentPrepared(), true, false, nil
	}
	return prepared, true, false, nil
}

func (m ContextManager) foldContext(ctx context.Context, prepared PreparedContext, policy ContextPreparePolicy, inputHash string, est, fold, force int, forceFold bool) (PreparedContext, bool, error) {
	a := m.agent
	outcome, err := a.compactToProjection(ctx, policy.Trigger, policy.Instructions, forceFold)
	if err != nil {
		if errors.Is(err, errCompressStaleContext) && policy.Trigger != CompactionTriggerManual {
			return PreparedContext{}, true, nil
		}
		reason := fmt.Sprintf("context summary failed: %v", err)
		a.recordContextMaintenanceBlocked(inputHash, policy.Trigger, "summary", reason)
		if policy.Trigger == CompactionTriggerManual {
			return PreparedContext{}, false, err
		}
		if policy.Trigger == CompactionTriggerOverflow || (sharesContextWindow(a.prov) && est >= a.hardInputCeiling()) {
			return PreparedContext{}, false, fmt.Errorf("%w: %w", ErrCompactionRequired, err)
		}
		return prepared, false, nil
	}
	if outcome == CompactionNoop {
		canonical, _ := a.session.snapshotMessagesVersion()
		if policy.Trigger == CompactionTriggerPressure && a.activeTurnStart(canonical) >= 0 {
			return prepared, false, nil
		}
		reason := "context is above the maintenance threshold but no foldable region remains"
		a.recordContextMaintenanceBlocked(inputHash, policy.Trigger, "summary", reason)
		if policy.Trigger == CompactionTriggerOverflow || policy.Force || est >= force || (sharesContextWindow(a.prov) && est >= a.hardInputCeiling()) {
			return PreparedContext{}, false, fmt.Errorf("%w: %s", ErrCompactionRequired, reason)
		}
		return prepared, false, nil
	}

	result := m.currentPrepared()
	if policy.Trigger == CompactionTriggerManual {
		return result, false, nil
	}
	if result.InputTokens >= fold {
		reason := fmt.Sprintf("summary result remains above fold trigger (%d >= %d)", result.InputTokens, fold)
		a.recordContextMaintenanceBlocked(a.contextMaintenanceInputHash(result.Messages), policy.Trigger, "summary", reason)
		a.compactStuck = true
		a.consecutiveCompacts++
		if policy.Trigger == CompactionTriggerOverflow || (sharesContextWindow(a.prov) && result.InputTokens >= a.hardInputCeiling()) {
			return PreparedContext{}, false, fmt.Errorf("%w: %s", ErrCompactionRequired, reason)
		}
		slog.Info("agent: context maintenance paused below hard ceiling", "reason", reason)
	}
	return result, false, nil
}

func (m ContextManager) currentPrepared() PreparedContext {
	if m.agent == nil {
		return PreparedContext{}
	}
	visible := m.agent.modelVisibleMessages()
	return PreparedContext{
		Messages:          append([]provider.Message(nil), visible...),
		InputTokens:       m.agent.estimatedPromptTokens(visible),
		ProjectionVersion: m.agent.currentProjectionVersion(),
	}
}
