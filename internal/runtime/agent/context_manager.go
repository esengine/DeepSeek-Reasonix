package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"reasonix/internal/contract/provider"
)

// compactionProgress is how compaction is faring in this session: whether a
// fold stopped reducing, and the turn the last one committed under. Both are
// cleared together whenever the lineage resets, which is why they travel as
// one value rather than two fields.
type compactionProgress struct {
	stuck bool // a fold landed above the trigger, so pressure retries are pointless
	// lastReported is the verdict already reported for the running turn. A crossed
	// threshold stays crossed, so without it every later round of the same turn
	// would report the same "folded nothing" again.
	lastReported maintenanceReport
	// lastUserTurns is what the most recent fold could and could not hold of
	// the user's own words. The notice fires once, at the moment of the fold;
	// this is what /context can still answer with afterwards.
	lastUserTurns userTurnRetention
}

// maintenanceReport is one reported no-fold verdict, scoped to the turn it was
// reached under: the same reason in a later turn is news again.
type maintenanceReport struct {
	reason CompactionNoopReason
	turn   int64
}

// restart clears what a new conversation or a new turn must not inherit.
// lastUserTurns stays: /context still answers with what the last fold held.
func (p *compactionProgress) restart() {
	p.stuck = false
	p.lastReported = maintenanceReport{}
}

// ContextManager is the sole owner of provider-visible context maintenance.
// Canonical session messages are immutable inputs; Prepare evolves only the
// durable projection and returns the exact visible view for one sampling round.
type ContextManager struct {
	agent *contextWindow
}

// ContextPreparePolicy describes one maintenance transaction.
type ContextPreparePolicy struct {
	Trigger      string
	Instructions string
	// IgnoreThreshold folds without waiting for the automatic trigger.
	// IgnoreEconomics folds even when the saving does not pay for it. One bool
	// for both let a hand-typed compact re-summarize an unchanged context.
	IgnoreThreshold bool
	IgnoreEconomics bool
	// ObservedInputTokens pins the input size instead of estimating it. Tests use
	// it to trigger a fold at an exact size; production reads the calibrated
	// shape, which is anchored on real provider counts.
	ObservedInputTokens int
}

// PreparedContext is the frozen result of a successful Prepare transaction.
type PreparedContext struct {
	Messages          []provider.Message
	InputTokens       int
	ProjectionVersion uint64
	// Maintenance is what this preparation did to the context, and when it did
	// nothing, which economics declined it.
	Maintenance CompactVerdict
}

func (a *contextWindow) contextManager() ContextManager { return ContextManager{agent: a} }

// PrepareContext is the public automatic-maintenance entry used by smoke tools
// and controllers that need a one-shot Prepare without sampling.
func (a *Agent) PrepareContext(ctx context.Context) error {
	_, err := a.window().contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: CompactionTriggerPressure})
	return err
}

// Prepare is the sole automatic maintenance entry. Below compact_ratio it does
// nothing. At or above the trigger it runs one summary transaction that either
// installs a checkpoint or records a generation-scoped blocked/failed receipt.
func (m ContextManager) Prepare(ctx context.Context, policy ContextPreparePolicy) (PreparedContext, error) {
	if policy.Trigger == "" {
		policy.Trigger = CompactionTriggerPressure
	}
	return m.prepareOnce(ctx, policy)
}

func (m ContextManager) prepareOnce(ctx context.Context, policy ContextPreparePolicy) (PreparedContext, error) {
	a := m.agent
	if a == nil || a.sess.conversation == nil {
		return PreparedContext{}, nil
	}
	visible := a.modelVisibleMessages()
	// Threshold uses the stable pre-interceptor request shape (messages + tools
	// + role projection). Extension interceptors run only on the real sampling
	// request so side-effecting plugins are not double-invoked; if they expand
	// the prompt past the hard ceiling, overflow recovery still fires.
	visibleShape := a.visibleRequestShape(visible)
	est := a.estimatedShapeTokens(visibleShape)
	ownEst := est // before an observation that may count provider-injected content
	prepared := PreparedContext{
		Messages:          append([]provider.Message(nil), visible...),
		InputTokens:       est,
		ProjectionVersion: a.currentProjectionVersion(),
	}
	if a.effectiveContextWindow() <= 0 || len(visible) == 0 {
		return prepared, nil
	}
	fold := a.compactTrigger()
	hard := a.hardInputCeiling()
	// pressure only decides whether to fold; the hard ceiling stays on the local
	// estimate, because a provider count no fold can lower must not end the turn.
	pressure := est
	if policy.ObservedInputTokens > 0 {
		est = policy.ObservedInputTokens
		prepared.InputTokens = est
		pressure = est
	} else if floor, ok := a.providerReportedFloor(visibleShape, prepared.ProjectionVersion); ok {
		pressure = max(pressure, floor)
	}
	inputHash := a.contextMaintenanceInputHash(visible)
	if blocked, reason := a.contextMaintenanceBlocked(inputHash, ownEst, policy.Trigger == CompactionTriggerOverflow); blocked && policy.Trigger != CompactionTriggerManual {
		// A generation that freed nothing has nothing left to try, so the
		// request goes out and the provider rules.
		if policy.Trigger == CompactionTriggerOverflow {
			return PreparedContext{}, fmt.Errorf("%w: %s", ErrCompactionRequired, reason)
		}
		if policy.Trigger == CompactionTriggerPressure && ownEst >= fold {
			a.noteMaintenanceHeld()
		}
		return prepared, nil
	}
	if pressure < fold {
		a.sess.win.compaction.stuck = false
	}
	if a.sess.win.compaction.stuck && policy.Trigger == CompactionTriggerPressure {
		return prepared, nil
	}
	// Asking, overflow and a physical ceiling each waive the trigger. Only the
	// last two, and an explicit force, waive what the fold has to be worth.
	scope := compactionScope{
		ignoreThreshold: policy.IgnoreThreshold || policy.Trigger == CompactionTriggerManual ||
			policy.Trigger == CompactionTriggerOverflow || est >= hard,
		ignoreEconomics: policy.IgnoreEconomics || policy.Trigger == CompactionTriggerOverflow || est >= hard,
	}
	if pressure < fold && !scope.ignoreThreshold {
		return prepared, nil
	}

	return m.foldContext(ctx, prepared, policy, inputHash, est, ownEst, fold, hard, scope)
}

func (m ContextManager) foldContext(ctx context.Context, prepared PreparedContext, policy ContextPreparePolicy, inputHash string, est, ownEst, fold, hard int, scope compactionScope) (PreparedContext, error) {
	a := m.agent
	// Where this function would answer ErrCompactionRequired, the fold is the
	// only way out and a failed summary must degrade rather than strand the turn.
	mustFree := policy.Trigger != CompactionTriggerManual && (policy.Trigger == CompactionTriggerOverflow || est >= hard)
	outcome, noopReason, err := a.compactToProjection(ctx, policy.Trigger, policy.Instructions, scope, mustFree)
	if err != nil {
		// Cancellation is the caller's decision, not a summary that failed:
		// recording it as one would blame the summarizer for this generation,
		// and degrading around it would fold a view nobody is waiting for.
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return PreparedContext{}, err
		}
		if errors.Is(err, errCompressStaleContext) && policy.Trigger != CompactionTriggerManual {
			// Transcript changed during the summary call: discard the candidate
			// and block this generation so we do not pay for a second summary.
			reason := "context changed during summary; automatic retry blocked for this generation"
			a.recordContextMaintenanceBlocked(inputHash, policy.Trigger, "summary", FailContextChanged, reason)
			if policy.Trigger == CompactionTriggerOverflow || est >= hard {
				return PreparedContext{}, fmt.Errorf("%w: %s", ErrCompactionRequired, reason)
			}
			return prepared, nil
		}
		status := "failed"
		if errors.Is(err, errSummaryOutputTruncated) || errors.Is(err, errCheckpointRejected) {
			status = "blocked"
		}
		reason := fmt.Sprintf("context summary failed: %v", err)
		a.recordContextMaintenanceOutcome(inputHash, policy.Trigger, "summary", status, compactionFailureCode(err), reason)
		if policy.Trigger == CompactionTriggerManual {
			return PreparedContext{}, err
		}
		if policy.Trigger == CompactionTriggerOverflow || est >= hard {
			// The overflow may not be ours to fold: refusing sends no request, so
			// the observation never updates and every later turn decides the same.
			// When our own transcript fits, let the provider rule instead.
			if ownEst < hard {
				return prepared, nil
			}
			return PreparedContext{}, fmt.Errorf("%w: %w", ErrCompactionRequired, err)
		}
		return prepared, nil
	}
	if outcome == CompactionNoop {
		canonical, _ := a.sess.conversation.SnapshotMessagesVersion()
		if policy.Trigger == CompactionTriggerPressure && a.activeTurnStart(canonical) >= 0 {
			// Saying so is the only record that the attempt happened; this
			// path deliberately does not block the next one.
			a.noteMaintenanceNoop(policy.Trigger, noopReason, est)
			return prepared, nil
		}
		reason := "context is above the maintenance threshold but no foldable region remains"
		if policy.Trigger == CompactionTriggerManual {
			reason = "no foldable region remains"
		}
		a.recordContextMaintenanceBlocked(inputHash, policy.Trigger, "summary", noopReason, reason)
		// A request that was declined is answered, not failed: the caller gets
		// the reason the host already settled and says so in its own words.
		// Only a fold nothing else can free is an error.
		if policy.Trigger == CompactionTriggerManual && est < hard {
			prepared.Maintenance = CompactVerdict{Reason: noopReason}
			return prepared, nil
		}
		if policy.Trigger == CompactionTriggerOverflow || est >= hard {
			return PreparedContext{}, fmt.Errorf("%w: %s", ErrCompactionRequired, reason)
		}
		prepared.Maintenance = CompactVerdict{Reason: noopReason}
		return prepared, nil
	}

	result := m.currentPrepared()
	result.Maintenance = CompactVerdict{Installed: outcome == CompactionInstalled}
	if policy.Trigger == CompactionTriggerManual {
		return result, nil
	}
	if result.InputTokens >= fold {
		reason := fmt.Sprintf("summary result remains above fold trigger (%d >= %d)", result.InputTokens, fold)
		a.recordContextMaintenanceBlocked(a.contextMaintenanceInputHash(result.Messages), policy.Trigger, "summary", FailResultAboveTrigger, reason)
		a.sess.win.compaction.stuck = true
		// Only a provider that already refused ends the turn here. Our ceiling
		// is an estimate, and refusing on it turns a window too small to
		// summarize one turn into a failed run the provider would have served.
		if policy.Trigger == CompactionTriggerOverflow {
			return PreparedContext{}, fmt.Errorf("%w: %s", ErrCompactionRequired, reason)
		}
		slog.Info("agent: context maintenance paused above the fold trigger", "reason", reason)
	}
	return result, nil
}

func (m ContextManager) currentPrepared() PreparedContext {
	if m.agent == nil {
		return PreparedContext{}
	}
	visible := m.agent.modelVisibleMessages()
	return PreparedContext{
		Messages:          append([]provider.Message(nil), visible...),
		InputTokens:       m.agent.estimatedVisibleRequestTokens(visible),
		ProjectionVersion: m.agent.currentProjectionVersion(),
	}
}

// estimatedVisibleRequestTokens sizes the pre-interceptor sampling shape:
// ModelMessages + role projection + tool schemas. Extension interceptors are
// intentionally omitted here (see prepareOnce) to avoid double side effects.
func (a *contextWindow) estimatedVisibleRequestTokens(visible []provider.Message) int {
	return a.estimatedShapeTokens(a.visibleRequestShape(visible))
}

func (a *contextWindow) visibleRequestShape(visible []provider.Message) requestCalibrationShape {
	if a == nil {
		return requestCalibrationShape{}
	}
	msgs := a.providerProjectionMessages(provider.ModelMessages(append([]provider.Message(nil), visible...)))
	for i := range msgs {
		msgs[i].CreatedAt = 0
	}
	return a.requestCalibrationShape(provider.Request{
		Messages:    msgs,
		Tools:       a.estimationSurface(),
		MaxTokens:   a.maxOutputTokens,
		Temperature: provider.OptionalTemperature(a.temperature),
	})
}
