package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"reasonix/internal/provider"
)

// compactionProgress is how compaction is faring in this session: whether a
// fold stopped reducing, how many ran back to back, and which retries already
// ran in the active turn. The fields are cleared together on lineage resets.
type compactionProgress struct {
	stuck          bool   // a fold landed above the trigger, so the same-view pressure retry is pointless
	stuckInputHash string // provider-visible view covered by stuck; changed input may retry
	consecutive    int    // back-to-back folds since one last helped
	// failedTurn backs off changed-view retries within one active tool loop.
	// A later user turn may retry, while hard-ceiling recovery bypasses it.
	failedTurn atomic.Int64
	// lastTurn stops the post-turn observer and the pre-send preflight from
	// paying for two summaries during one active tool loop.
	lastTurn atomic.Int64
}

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
// nothing. At or above the trigger it runs one single-flight prune/summary
// transaction, with at most two successful summary attempts under pressure.
func (m ContextManager) Prepare(ctx context.Context, policy ContextPreparePolicy) (PreparedContext, error) {
	if policy.Trigger == "" {
		policy.Trigger = CompactionTriggerPressure
	}
	if m.agent == nil {
		return PreparedContext{}, nil
	}
	m.agent.sess.compactionRunMu.Lock()
	defer m.agent.sess.compactionRunMu.Unlock()
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
	// Receipts back off sub-critical retries only. Physical overflow may retry
	// maintenance once, but a failed summary never fabricates fallback content.
	if blocked, _ := a.contextMaintenanceBlocked(inputHash); blocked && policy.Trigger != CompactionTriggerManual &&
		policy.Trigger != CompactionTriggerOverflow && est < hard {
		return prepared, nil
	}
	if est < fold {
		a.sess.compaction.consecutive = 0
		a.sess.compaction.stuck = false
		a.sess.compaction.stuckInputHash = ""
		a.sess.compaction.failedTurn.Store(0)
	}
	if a.sess.compaction.stuck && a.sess.compaction.stuckInputHash != inputHash {
		// The previous projection could not reclaim enough from its exact view,
		// but newly appended messages create a new fold boundary and may retry.
		a.sess.compaction.stuck = false
		a.sess.compaction.stuckInputHash = ""
		a.sess.compaction.consecutive = 0
	}
	if a.sess.compaction.stuck && policy.Trigger == CompactionTriggerPressure && est < hard {
		return prepared, nil
	}
	// One user trigger. Overflow is a one-shot physical recovery path only.
	forceFold := policy.Force || policy.Trigger == CompactionTriggerManual || policy.Trigger == CompactionTriggerOverflow || est >= hard
	if est < fold && !forceFold {
		return prepared, nil
	}

	if policy.Trigger == CompactionTriggerPressure || policy.Trigger == CompactionTriggerOverflow {
		applied, err := a.pruneToolResultsToProjectionLocked(policy.Trigger)
		if err != nil {
			return PreparedContext{}, err
		}
		if applied {
			prepared = m.currentPrepared()
			est = prepared.InputTokens
			inputHash = a.contextMaintenanceInputHash(prepared.Messages)
			if (policy.Trigger == CompactionTriggerPressure && est < fold) ||
				(policy.Trigger == CompactionTriggerOverflow && est < hard) {
				return prepared, nil
			}
		}
	}

	return m.foldContext(ctx, prepared, policy, inputHash, est, fold, hard, forceFold)
}

func (m ContextManager) foldContext(ctx context.Context, prepared PreparedContext, policy ContextPreparePolicy, inputHash string, est, fold, hard int, forceFold bool) (PreparedContext, error) {
	a := m.agent
	maxSummaries := 1
	if policy.Trigger == CompactionTriggerPressure {
		maxSummaries = 2
	}
	result := prepared
	for range maxSummaries {
		mustFree := policy.Trigger != CompactionTriggerManual && (policy.Trigger == CompactionTriggerOverflow || result.InputTokens >= hard)
		outcome, err := a.compactToProjectionLocked(ctx, policy.Trigger, policy.Instructions, forceFold, mustFree)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return PreparedContext{}, err
			}
			if errors.Is(err, errCompressStaleContext) && policy.Trigger != CompactionTriggerManual {
				reason := "context changed during summary; automatic retry blocked for this generation"
				a.recordContextMaintenanceBlocked(inputHash, policy.Trigger, "summary", reason)
				if policy.Trigger == CompactionTriggerOverflow || result.InputTokens >= hard {
					return PreparedContext{}, fmt.Errorf("%w: %s", ErrCompactionRequired, reason)
				}
				return m.currentPrepared(), nil
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
			latest := m.currentPrepared()
			if policy.Trigger == CompactionTriggerOverflow || latest.InputTokens >= hard {
				return PreparedContext{}, fmt.Errorf("%w: %w", ErrCompactionRequired, err)
			}
			return latest, nil
		}
		if outcome == CompactionNoop {
			reason := "context is above the maintenance threshold but no foldable region remains"
			a.recordContextMaintenanceBlocked(inputHash, policy.Trigger, "summary", reason)
			latest := m.currentPrepared()
			if policy.Trigger == CompactionTriggerOverflow || policy.Force || latest.InputTokens >= hard {
				return PreparedContext{}, fmt.Errorf("%w: %s", ErrCompactionRequired, reason)
			}
			return latest, nil
		}

		result = m.currentPrepared()
		if policy.Trigger == CompactionTriggerManual || result.InputTokens < fold ||
			(policy.Trigger == CompactionTriggerOverflow && result.InputTokens < hard) {
			a.sess.compaction.stuck = false
			a.sess.compaction.stuckInputHash = ""
			a.sess.compaction.consecutive = 0
			a.sess.compaction.failedTurn.Store(0)
			return result, nil
		}
		forceFold = false
		inputHash = a.contextMaintenanceInputHash(result.Messages)
	}

	reason := fmt.Sprintf("summary result remains above fold trigger after %d attempts (%d >= %d)", maxSummaries, result.InputTokens, fold)
	blockedInputHash := a.contextMaintenanceInputHash(result.Messages)
	a.recordContextMaintenanceBlocked(blockedInputHash, policy.Trigger, "summary", reason)
	a.sess.compaction.stuck = true
	a.sess.compaction.stuckInputHash = blockedInputHash
	a.sess.compaction.consecutive += maxSummaries
	if policy.Trigger == CompactionTriggerOverflow || result.InputTokens >= hard {
		return PreparedContext{}, fmt.Errorf("%w: %s", ErrCompactionRequired, reason)
	}
	slog.Info("agent: context maintenance paused below hard ceiling", "reason", reason)
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
	msgs := a.normalizeModelRequestMessages(visible)
	tools := a.providerToolSchemas()
	return a.estimatedRequestTokens(provider.Request{
		Messages:    msgs,
		Tools:       tools,
		MaxTokens:   a.maxOutputTokens,
		Temperature: provider.OptionalTemperature(a.temperature),
	})
}
