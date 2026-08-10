package agent

import (
	"context"
	"encoding/json"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// samplingRequest is a once-prepared, frozen provider request for one model
// round. All stream retries replay this exact payload — no synthetic recovery
// messages, no schema reorder, no previous_response_id drift from failed attempts.
type samplingRequest struct {
	req provider.Request
}

func (a *Agent) streamProviderRequest(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return a.prov.Stream(ctx, req)
}

func (a *Agent) handleSamplingError(
	ctx context.Context,
	attemptID string,
	attempt int,
	streamSink *deferredStreamSink,
	frozen *samplingRequest,
	result, last streamedTurn,
	billable *provider.Usage,
) (retry bool, terminal streamedTurn) {
	if provider.IsStreamInterrupted(result.err) && attempt < maxSamplingAttempts {
		streamSink.Discard()
		reason := provider.StreamInterruptReason(result.err)
		a.emitStreamAttempt(attemptID, event.StreamAttemptDiscard, attempt, reason, result.err)
		a.sink.Emit(event.Event{
			Kind: event.Retrying, RetryAttempt: attempt, RetryMax: maxStreamRecoveries,
			RetryScope: event.RetryScopeStream,
		})
		if !streamRetrySleep(ctx, attempt) {
			return false, streamedTurn{usage: finalizeSamplingUsage(billable, result.usage), interrupted: true, err: ctx.Err()}
		}
		return true, streamedTurn{}
	}
	// Exhausted retries or non-retryable error: leave the last speculative UI
	// visible (no discard) so LocalOnly can mirror it.
	streamSink.Flush()
	last.usage = finalizeSamplingUsage(billable, result.usage)
	return false, last
}

// prepareSamplingRequest freezes one model-round request (preflight + interceptors).
// Output budgets are resolved only here and never change the compact_ratio
// trigger. Physical overflow may attempt at most one recovery summary.
func (a *Agent) prepareSamplingRequest(ctx context.Context) (samplingRequest, error) {
	frozen, err := a.buildSamplingRequest(ctx, CompactionTriggerPressure)
	if err != nil {
		return samplingRequest{}, err
	}
	if budget, clipped, budgetErr := a.effectiveOutputBudget(frozen.req); budgetErr != nil {
		// One-shot physical overflow recovery. Do not loop.
		if _, perr := a.contextManager().Prepare(ctx, ContextPreparePolicy{
			Trigger: CompactionTriggerOverflow,
			Force:   true,
		}); perr != nil {
			return samplingRequest{}, budgetErr
		}
		rebuilt, rerr := a.buildSamplingRequest(ctx, CompactionTriggerPressure)
		if rerr != nil {
			return samplingRequest{}, rerr
		}
		if _, _, budgetErr2 := a.effectiveOutputBudget(rebuilt.req); budgetErr2 != nil {
			return samplingRequest{}, budgetErr2
		}
		// Re-apply clipping on the recovered view.
		if budget2, clipped2, err2 := a.effectiveOutputBudget(rebuilt.req); err2 == nil && clipped2 {
			rebuilt.req.MaxTokens = budget2
		}
		shape := a.requestCalibrationShape(rebuilt.req)
		a.activeReqShape.Store(&shape)
		return samplingRequest{req: freezeProviderRequest(rebuilt.req)}, nil
	} else if clipped {
		frozen.req.MaxTokens = budget
	}
	shape := a.requestCalibrationShape(frozen.req)
	a.activeReqShape.Store(&shape)
	return samplingRequest{req: freezeProviderRequest(frozen.req)}, nil
}

func (a *Agent) buildSamplingRequest(ctx context.Context, trigger string) (samplingRequest, error) {
	// CreatedAt is durable UI metadata, not model input. Strip it from the
	// transport copy so wall-clock differences never invalidate the provider's
	// prompt-cache prefix (and custom providers cannot accidentally send it).
	prepared, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: trigger})
	if err != nil {
		return samplingRequest{}, err
	}
	requestMessages := append([]provider.Message(nil), provider.ModelMessages(prepared.Messages)...)
	requestMessages = a.providerProjectionMessages(requestMessages)
	for i := range requestMessages {
		requestMessages[i].CreatedAt = 0
	}
	// context.prepare: extensions may rewrite the message copy feeding THIS
	// request. The session log is never touched — the replacement is
	// ephemeral, so the next request starts from the unmodified history.
	requestMessages, err = a.interceptContextPrepare(ctx, requestMessages)
	if err != nil {
		return samplingRequest{}, err
	}
	req := provider.Request{
		Messages:       requestMessages,
		Tools:          a.tools.Schemas(),
		MaxTokens:      a.maxOutputTokens,
		Temperature:    provider.OptionalTemperature(a.temperature),
		ResponseFormat: responseFormatFromRequest(ctx),
		EffortOverride: a.governorOverride(),
	}
	// provider.request: the fully assembled request gets one last ruling
	// (revalidated by the payload registry) before it goes on the wire.
	req, err = a.interceptProviderRequest(ctx, req)
	if err != nil {
		return samplingRequest{}, err
	}
	return samplingRequest{req: req}, nil
}

// providerProjectionMessages applies provider-specific role compatibility to a
// request copy. Projection sidecars retain logical user-turn boundaries so
// explicit range compression can continue to resolve anchors across calls.
func (a *Agent) providerProjectionMessages(msgs []provider.Message) []provider.Message {
	if a != nil && a.strictAlternatingRoles {
		return coalesceProjectionUserRuns(msgs)
	}
	return msgs
}

// freezeProviderRequest deep-copies the provider-visible request surface so
// retries share identical messages, tools order, temperature, and format.
func freezeProviderRequest(req provider.Request) provider.Request {
	out := req
	if len(req.Messages) > 0 {
		out.Messages = append([]provider.Message(nil), req.Messages...)
		for i := range out.Messages {
			if len(out.Messages[i].ToolCalls) > 0 {
				out.Messages[i].ToolCalls = append([]provider.ToolCall(nil), out.Messages[i].ToolCalls...)
			}
			if len(out.Messages[i].Images) > 0 {
				out.Messages[i].Images = append([]string(nil), out.Messages[i].Images...)
			}
			if len(out.Messages[i].ResponsesItems) > 0 {
				items := make([]json.RawMessage, len(out.Messages[i].ResponsesItems))
				for j, item := range out.Messages[i].ResponsesItems {
					items[j] = append(json.RawMessage(nil), item...)
				}
				out.Messages[i].ResponsesItems = items
			}
		}
	}
	if len(req.Tools) > 0 {
		out.Tools = make([]provider.ToolSchema, len(req.Tools))
		for i, schema := range req.Tools {
			out.Tools[i] = schema
			if len(schema.Parameters) > 0 {
				out.Tools[i].Parameters = append(json.RawMessage(nil), schema.Parameters...)
			}
		}
	}
	if req.Temperature != nil {
		t := *req.Temperature
		out.Temperature = &t
	}
	if req.ResponseFormat != nil {
		rf := *req.ResponseFormat
		out.ResponseFormat = &rf
	}
	return out
}
