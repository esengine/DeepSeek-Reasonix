package agent

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type samplingRecoveryState struct {
	frozen                             samplingRequest
	context                            contextRecoveryBudget
	replay                             reasoningReplayRecoveryBudget
	output, protocol, partial, missing bool
	billable                           *provider.Usage
	waited                             time.Duration
}

func (a *Agent) samplingDeadline(ctx context.Context) (context.Context, context.CancelFunc, TaskBudget) {
	limit := a.taskBudgetLimit(ctx)
	if a.turn.graceRound {
		limit = TaskBudget{}
	}
	if limit.Wall <= 0 {
		return ctx, func() {}, limit
	}
	started := a.task.budget.started
	if started.IsZero() {
		started = a.turn.budget.started
	}
	if started.IsZero() {
		started = time.Now()
	}
	next, cancel := context.WithDeadline(ctx, started.Add(limit.Wall))
	return next, cancel, limit
}

func (a *Agent) streamWithSamplingRecovery(parent context.Context, turn int) (terminal streamedTurn) {
	ctx, cancel, limit := a.samplingDeadline(parent)
	defer cancel()
	state := samplingRecoveryState{}
	defer func() {
		if limit.Wall > 0 && errors.Is(terminal.err, context.DeadlineExceeded) && parent.Err() == nil {
			terminal.err = &taskBudgetPause{axis: "time", detail: "recovery reached the task deadline"}
		}
		if terminal.err == nil && state.replay.retries > 0 {
			a.activateReasoningReplayStrongProjection(state.replay)
		}
	}()
	var err error
	state.frozen, err = a.prepareSamplingRequest(ctx)
	if err != nil {
		return streamedTurn{err: err}
	}
	if err := a.consumeManualProtocolRecovery(ctx, &state); err != nil {
		return streamedTurn{err: err}
	}
	ctx = provider.WithManagedRecovery(provider.WithRequestAttemptCounter(ctx))
	for attempt := 1; ; attempt++ {
		if err := a.samplingRecoveryStop(ctx, limit, state.billable, attempt); err != nil {
			return streamedTurn{err: err, usage: state.billable}
		}
		if state.protocol && !state.replay.persisted {
			record := a.protocolRecord(state.frozen, "consumed")
			if state.replay.cutoff > 0 {
				record.Projected = true
				record.Prefix, record.Anchor = state.replay.cutoff, state.replay.anchor
			}
			if err := a.saveProtocolRecord(record); err != nil {
				return streamedTurn{err: err, usage: state.billable}
			}
			state.replay.persisted = true
		}
		id := newStreamAttemptID(attempt)
		a.emitStreamAttempt(id, event.StreamAttemptBegin, attempt, "", nil)
		sink, attemptSink := a.samplingAttemptSinks()
		result := a.runSamplingAttempt(ctx, turn, attemptSink, &state.frozen, id)
		state.billable, _ = a.recordSamplingAttempt(state.billable, result)
		if ctx.Err() != nil {
			sink.Discard()
			return streamedTurn{err: ctx.Err(), interrupted: true, usage: state.billable}
		}
		if result.err == nil {
			retry, done := a.handleSamplingCandidate(&state, result, sink, attempt, id)
			if retry {
				continue
			}
			return done
		}
		state.partial = state.partial || sawSpeculativeSamplingOutput(result) || len(result.responsesItems) > 0 || len(result.serverSearch) > 0
		if attempt < maxSamplingAttempts && a.trySamplingRepair(ctx, &state, result, sink, attempt, id) {
			continue
		}
		if a.waitSamplingRetry(ctx, &state, result, sink, attempt, id) {
			continue
		}
		sink.Flush()
		if !state.protocol {
			if err := a.offerProtocolRecovery(state.frozen, result.err); err != nil {
				result.err = err
			}
		}
		if provider.AsContextLimitError(result.err) != nil {
			a.setLastRecovery(contextRecoveryFailed)
		}
		result.usage = finalizeSamplingUsage(state.billable, result.usage)
		if ctx.Err() != nil {
			result.err = ctx.Err()
			result.interrupted = true
		}
		return result
	}
}

func (a *Agent) samplingRecoveryStop(ctx context.Context, limit TaskBudget, usage *provider.Usage, attempt int) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if attempt <= 1 {
		return nil
	}
	shadow := a.task.budget
	if usage != nil {
		shadow.observe(usage, a.svc.pricing)
	}
	if axis, detail := shadow.exceeded(limit); axis != "" {
		return &taskBudgetPause{axis: axis, detail: detail}
	}
	return nil
}

func (a *Agent) handleSamplingCandidate(s *samplingRecoveryState, result streamedTurn, sink *deferredStreamSink, attempt int, id string) (bool, streamedTurn) {
	issue := a.reasoningReplayIssue(result)
	if issue == "" {
		a.observeMissingAssistantReasoning(result.assistantMessage(), result.reasoningComplete)
		if s.missing {
			a.recordRecoveredCandidate(result)
		}
		sink.Flush()
		a.emitStreamAttempt(id, event.StreamAttemptCommit, attempt, "", nil)
		result.usage = finalizeSamplingUsage(s.billable, result.usage)
		return false, result
	}
	s.partial = true
	_, claimed := a.observeMissingAssistantReasoning(result.assistantMessage(), result.reasoningComplete)
	if (issue != ReasoningReplayMissing && issue != ReasoningReplayIncomplete) || s.protocol || a.protocolRecoverySpent() || !claimed || attempt >= maxSamplingAttempts {
		return false, a.finishReasoningReplayOverflow(result, sink, issue, s.billable, id, attempt)
	}
	s.protocol, s.missing = true, true
	event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetryAttempted})
	if next, ok := a.recoverReasoningReplayHistory(s.frozen, &s.replay); ok {
		s.frozen = next
		s.replay.local = true
	}
	sink.Discard()
	a.emitStreamAttempt(id, event.StreamAttemptDiscard, attempt, "reasoning_replay", nil)
	a.emitProtocolRetry(attempt, false)
	return true, streamedTurn{}
}

func (a *Agent) recordRecoveredCandidate(result streamedTurn) {
	kind := event.ProtocolRecoveryMissingReasoningRetryRecovered
	if len(result.calls) == 0 && len(result.serverSearch) == 0 {
		kind = event.ProtocolRecoveryMissingReasoningRetryReplaced
	}
	event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: kind})
}

func (a *Agent) trySamplingRepair(ctx context.Context, s *samplingRecoveryState, result streamedTurn, sink *deferredStreamSink, attempt int, id string) bool {
	if limit := provider.AsOutputLimitError(result.err); !s.output && limit != nil && s.frozen.req.MaxTokens > limit.MaxOutputTokens {
		s.output = true
		a.learnOutputBudget(limit.MaxOutputTokens)
		s.frozen.req.MaxTokens = limit.MaxOutputTokens
		sink.Discard()
		a.emitStreamAttempt(id, event.StreamAttemptDiscard, attempt, "output_limit", result.err)
		return true
	}
	if next, ok, _ := a.recoverContextLimit(ctx, s.frozen, result.err, &s.context); ok {
		sink.Discard()
		a.emitStreamAttempt(id, event.StreamAttemptDiscard, attempt, "context_limit", result.err)
		s.frozen = next
		return true
	}
	if s.protocol {
		return false
	}
	next, ok := a.tryRecoverReasoningReplay400(sink, s.frozen, id, attempt, result.err, &s.replay)
	if ok {
		s.protocol = true
		s.frozen = next
	}
	return ok
}

func (a *Agent) canWaitSampling(ctx context.Context, s *samplingRecoveryState, f provider.RecoveryFailure) bool {
	role, _ := ctx.Value(turnContextRoleKey{}).(turnContextRole)
	if role == turnContextPlanner {
		return false
	}
	if SubagentDepth(ctx) != 0 || a.turn.graceRound || a.turn.recoveryGraceRound || s.partial || len(a.turn.writeRecovery) > 0 {
		return false
	}
	return f.Retryable && (f.Phase == "connect" || (f.Phase == "headers" && (f.Status == 408 || f.Status == 429 || f.Status >= 500)))
}

func (a *Agent) waitSamplingRetry(ctx context.Context, s *samplingRecoveryState, result streamedTurn, sink *deferredStreamSink, attempt int, id string) bool {
	failure := provider.ClassifyRecovery(result.err)
	waiting := attempt >= maxSamplingAttempts && a.canWaitSampling(ctx, s, failure)
	if !failure.Retryable || (attempt >= maxSamplingAttempts && !waiting) {
		return false
	}
	base := time.Duration(1<<min(attempt-1, 2)) * 2 * time.Second
	delay := base
	if waiting {
		delay = time.Minute + time.Duration(rand.Intn(6001))*time.Millisecond
	}
	delay = max(delay, failure.RetryAfter)
	sink.Discard()
	reason := failure.Phase
	if provider.IsStreamInterrupted(result.err) {
		reason = provider.StreamInterruptReason(result.err)
	}
	a.emitStreamAttempt(id, event.StreamAttemptDiscard, attempt, reason, result.err)
	a.svc.sink.Emit(event.Event{Kind: event.Retrying, RetryAttempt: attempt, RetryMax: maxStreamRecoveries, RetryScope: event.RetryScopeStream, Recovery: &event.RecoveryStatus{Phase: failure.Phase, Reason: failure.Code, NextAttemptAt: time.Now().Add(delay).UnixMilli(), WaitedMs: s.waited.Milliseconds(), Waiting: waiting}})
	s.waited += delay
	if !waiting && failure.RetryAfter <= base {
		return streamRetrySleep(ctx, attempt)
	}
	return recoverySleep(ctx, delay)
}

func unmeteredHeaderFailure(result streamedTurn, httpRequests int) bool {
	if httpRequests <= 0 || sawSpeculativeSamplingOutput(result) {
		return false
	}
	failure := provider.ClassifyRecovery(result.err)
	return failure.Phase == "headers" || failure.Phase == "connect"
}

func unmeteredUsage(usage *provider.Usage, result streamedTurn, httpRequests int) *provider.Usage {
	if usage == nil && unmeteredHeaderFailure(result, httpRequests) {
		return &provider.Usage{Unknown: true, RequestCount: httpRequests}
	}
	return usage
}
