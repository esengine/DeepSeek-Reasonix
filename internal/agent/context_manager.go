package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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

// PrepareContext is the public automatic-maintenance entry used by smoke tools
// and controllers that need a one-shot Prepare without sampling.
func (a *Agent) PrepareContext(ctx context.Context) error {
	_, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: CompactionTriggerPressure})
	return err
}

// ObserveUsage is retained as a compatibility hook. Usage observations never
// mutate the provider-visible checkpoint.
func (m ContextManager) ObserveUsage(u *provider.Usage) {
	_ = u
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
	if a == nil || a.session == nil {
		return PreparedContext{}, nil
	}
	visible := a.modelVisibleMessages()
	// Threshold uses the stable pre-interceptor request shape (messages + tools
	// + role projection). Extension interceptors run only on the real sampling
	// request so side-effecting plugins are not double-invoked; if they expand
	// the prompt past the hard ceiling, overflow recovery still fires.
	est := a.estimatedVisibleRequestTokens(visible)
	prepared := PreparedContext{
		Messages:          append([]provider.Message(nil), visible...),
		InputTokens:       est,
		ProjectionVersion: a.currentProjectionVersion(),
	}
	if a.contextWindow <= 0 || len(visible) == 0 {
		return prepared, nil
	}
	fold := a.compactTrigger()
	hard := a.hardInputCeiling()
	if policy.ObservedInputTokens > 0 {
		est = policy.ObservedInputTokens
		prepared.InputTokens = est
	}
	inputHash := a.contextMaintenanceInputHash(visible)
	if blocked, reason := a.contextMaintenanceBlocked(inputHash); blocked && policy.Trigger != CompactionTriggerManual {
		if policy.Trigger == CompactionTriggerOverflow || est >= hard {
			return PreparedContext{}, fmt.Errorf("%w: %s", ErrCompactionRequired, reason)
		}
		return prepared, nil
	}
	if est < fold {
		a.consecutiveCompacts = 0
		a.compactStuck = false
	}
	if a.compactStuck && policy.Trigger == CompactionTriggerPressure {
		return prepared, nil
	}
	// One user trigger. Overflow is a one-shot physical recovery path only.
	forceFold := policy.Force || policy.Trigger == CompactionTriggerManual || policy.Trigger == CompactionTriggerOverflow || est >= hard
	if est < fold && !forceFold {
		return prepared, nil
	}

	return m.foldContext(ctx, prepared, policy, inputHash, est, fold, hard, forceFold)
}

func (m ContextManager) foldContext(ctx context.Context, prepared PreparedContext, policy ContextPreparePolicy, inputHash string, est, fold, hard int, forceFold bool) (PreparedContext, error) {
	a := m.agent
	outcome, err := a.compactToProjection(ctx, policy.Trigger, policy.Instructions, forceFold)
	if err != nil {
		if errors.Is(err, errCompressStaleContext) && policy.Trigger != CompactionTriggerManual {
			// Transcript changed during the summary call: discard the candidate
			// and block this generation so we do not pay for a second summary.
			reason := "context changed during summary; automatic retry blocked for this generation"
			a.recordContextMaintenanceBlocked(inputHash, policy.Trigger, "summary", reason)
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
		a.recordContextMaintenanceOutcome(inputHash, policy.Trigger, "summary", status, reason)
		if policy.Trigger == CompactionTriggerManual {
			return PreparedContext{}, err
		}
		if policy.Trigger == CompactionTriggerOverflow || est >= hard {
			return PreparedContext{}, fmt.Errorf("%w: %w", ErrCompactionRequired, err)
		}
		return prepared, nil
	}
	if outcome == CompactionNoop {
		canonical, _ := a.session.snapshotMessagesVersion()
		if policy.Trigger == CompactionTriggerPressure && a.activeTurnStart(canonical) >= 0 {
			return prepared, nil
		}
		reason := "context is above the maintenance threshold but no foldable region remains"
		a.recordContextMaintenanceBlocked(inputHash, policy.Trigger, "summary", reason)
		if policy.Trigger == CompactionTriggerOverflow || policy.Force || est >= hard {
			return PreparedContext{}, fmt.Errorf("%w: %s", ErrCompactionRequired, reason)
		}
		return prepared, nil
	}

	result := m.currentPrepared()
	if policy.Trigger == CompactionTriggerManual {
		return result, nil
	}
	if result.InputTokens >= fold {
		reason := fmt.Sprintf("summary result remains above fold trigger (%d >= %d)", result.InputTokens, fold)
		a.recordContextMaintenanceBlocked(a.contextMaintenanceInputHash(result.Messages), policy.Trigger, "summary", reason)
		a.compactStuck = true
		a.consecutiveCompacts++
		if policy.Trigger == CompactionTriggerOverflow || result.InputTokens >= hard {
			return PreparedContext{}, fmt.Errorf("%w: %s", ErrCompactionRequired, reason)
		}
		slog.Info("agent: context maintenance paused below hard ceiling", "reason", reason)
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
func (a *Agent) estimatedVisibleRequestTokens(visible []provider.Message) int {
	if a == nil {
		return 0
	}
	msgs := a.providerProjectionMessages(provider.ModelMessages(append([]provider.Message(nil), visible...)))
	for i := range msgs {
		msgs[i].CreatedAt = 0
	}
	var tools []provider.ToolSchema
	if a.tools != nil {
		tools = a.tools.Schemas()
	}
	return a.estimatedRequestTokens(provider.Request{
		Messages:    msgs,
		Tools:       tools,
		MaxTokens:   a.maxOutputTokens,
		Temperature: provider.OptionalTemperature(a.temperature),
	})
}
