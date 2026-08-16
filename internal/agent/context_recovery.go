package agent

import (
	"context"
	"fmt"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type contextRecoveryBudget struct {
	budgetRetries  int
	compactRetries int
}

func (a *Agent) recoverContextLimit(ctx context.Context, frozen samplingRequest, err error, budget *contextRecoveryBudget) (samplingRequest, bool, string) {
	limit := provider.AsContextLimitError(err)
	if a == nil || limit == nil || budget == nil {
		return samplingRequest{}, false, contextRecoveryFailed
	}
	omitted := frozen.req.MaxTokens == 0
	if limit.PromptTokens > 0 {
		a.setPromptTokenCalibrationFromActive(limit.PromptTokens)
	}
	a.learnContextBudget(limit.WindowTokens, limit.CompletionTokens, omitted)
	adm := a.lastAdmission()
	adm.ObservedWindow = limit.WindowTokens
	adm.ObservedPrompt = limit.PromptTokens
	adm.ObservedCompletion = limit.CompletionTokens
	a.storeAdmission(adm)

	window := a.effectiveContextWindow()
	prompt := limit.PromptTokens
	if prompt <= 0 {
		prompt = a.estimatedRequestTokens(frozen.req)
	}
	physical := window - prompt - outputBudgetReserve
	if physical > 0 && budget.budgetRetries == 0 {
		next := freezeProviderRequest(frozen.req)
		next.MaxTokens = physical
		if frozen.req.MaxTokens > 0 && frozen.req.MaxTokens < physical {
			next.MaxTokens = frozen.req.MaxTokens
		}
		budget.budgetRetries++
		// Publish the request that will actually be retried, not the stale
		// pre-error admission. The Context Panel reads this atomic snapshot while
		// the turn is still active and after it completes.
		adm.WindowMode = provider.ContextWindowShared.String()
		adm.Source = provider.ContextBudgetSourceLearned
		adm.WindowTokens = window
		adm.PromptTokens = prompt
		adm.PhysicalRemaining = physical
		if adm.RequestedOutputTokens <= 0 {
			adm.RequestedOutputTokens = limit.CompletionTokens
		}
		if omitted && adm.AutoOutputTokens <= 0 {
			adm.AutoOutputTokens = limit.CompletionTokens
		}
		adm.EffectiveOutputTokens = next.MaxTokens
		adm.Clipped = adm.RequestedOutputTokens > 0 && next.MaxTokens < adm.RequestedOutputTokens
		adm.ApplyMaxTokens = next.MaxTokens > 0
		adm.LastRecovery = contextRecoveryLearnedRetry
		a.storeAdmission(adm)
		a.emitContextRecoveryNotice(contextRecoveryLearnedRetry, limit, next.MaxTokens)
		shape := a.requestCalibrationShape(next)
		a.sess.output.activeReqShape.Store(&shape)
		return samplingRequest{req: next}, true, contextRecoveryLearnedRetry
	}
	if budget.compactRetries == 0 {
		if _, perr := a.contextManager().Prepare(ctx, ContextPreparePolicy{
			Trigger: CompactionTriggerOverflow,
			Force:   true,
		}); perr != nil {
			a.setLastRecovery(contextRecoveryFailed)
			return samplingRequest{}, false, contextRecoveryFailed
		}
		rebuilt, rerr := a.buildSamplingRequest(ctx, CompactionTriggerPressure)
		if rerr != nil {
			a.setLastRecovery(contextRecoveryFailed)
			return samplingRequest{}, false, contextRecoveryFailed
		}
		if aerr := a.applyAdmissionToRequest(&rebuilt.req); aerr != nil {
			a.setLastRecovery(contextRecoveryFailed)
			return samplingRequest{}, false, contextRecoveryFailed
		}
		budget.compactRetries++
		a.setLastRecovery(contextRecoveryCompacted)
		a.emitContextRecoveryNotice(contextRecoveryCompacted, limit, rebuilt.req.MaxTokens)
		shape := a.requestCalibrationShape(rebuilt.req)
		a.sess.output.activeReqShape.Store(&shape)
		return samplingRequest{req: freezeProviderRequest(rebuilt.req)}, true, contextRecoveryCompacted
	}
	a.setLastRecovery(contextRecoveryFailed)
	return samplingRequest{}, false, contextRecoveryFailed
}

func (a *Agent) emitContextRecoveryNotice(kind string, limit *provider.ContextLimitError, nextOutput int) {
	if a == nil || a.svc.sink == nil {
		return
	}
	text := "Adjusted the output budget to fit the shared context window."
	if kind == contextRecoveryCompacted {
		text = "Compacted context after a shared-window overflow and retried."
	}
	detail := fmt.Sprintf("recovery=%s next_output=%d", kind, nextOutput)
	if limit != nil {
		detail = fmt.Sprintf("%s window=%d prompt=%d completion=%d requested=%d",
			detail, limit.WindowTokens, limit.PromptTokens, limit.CompletionTokens, limit.RequestedTokens)
	}
	a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: text, Detail: detail})
}
