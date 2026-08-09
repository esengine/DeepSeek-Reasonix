package agent

import (
	"context"
	"fmt"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// maxVisibleFinalRepairRounds bounds the host-owned rendering repair after a
// reasoning-only stop. These are additional sampling rounds after the model's
// original stop; repair rounds are finalization-only and may not execute tools.
const maxVisibleFinalRepairRounds = 2

func visibleFinalRepairMessage() string {
	return "The previous assistant response finished without any visible answer text. The work is already complete; this is a finalization-only repair. Do not call tools or repeat any work. Restate the completed result now as a concise visible answer only; do not send reasoning only."
}

// handleVisibleFinalContract enforces Content only for scoped/internal callers.
// Ordinary DeepSeek turns keep accepting a reasoning-only stop in one request.
func (a *Agent) handleVisibleFinalContract(ctx context.Context, state *runLoopState, text, reasoning string, usage *provider.Usage) (handled, cont bool, err error) {
	if hasVisibleFinalAnswer(text) {
		if state.visibleFinalRepair {
			state.visibleFinalRepair = false
			state.visibleFinalRepairRounds = 0
			state.emptyFinalBlocks = 0
		}
		return false, false, nil
	}
	if state.visibleFinalRepair {
		state.emptyFinalBlocks++
		if state.visibleFinalRepairRounds >= maxVisibleFinalRepairRounds {
			return true, false, visibleFinalRepairExhaustedError(state)
		}
		cont, err = a.requestVisibleFinalRepair(ctx, state, usage, len(reasoning))
		return true, cont, err
	}
	reasoningOnlyStop := reasoningOnlyFinishHonoured(a.prov, usage, reasoning)
	if state.requireVisibleFinal && reasoningOnlyStop {
		state.emptyFinalBlocks++
		state.visibleFinalRepair = true
		state.visibleFinalRepairRounds = 0
		cont, err = a.requestVisibleFinalRepair(ctx, state, usage, len(reasoning))
		return true, cont, err
	}
	if reasoningOnlyStop {
		return false, false, nil
	}
	state.emptyFinalBlocks++
	if state.emptyFinalBlocks >= maxEmptyFinalBlocks {
		return true, false, fmt.Errorf("model finished without a visible final answer %d times", state.emptyFinalBlocks)
	}
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeEmptyFinal, Text: emptyFinalNotice(), Detail: emptyFinalNoticeDetail(a.prov.Name(), usage, len(reasoning))})
	a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(emptyFinalRetryMessage())})
	a.maybeCompact(ctx, usage)
	return true, true, nil
}

func (a *Agent) requestVisibleFinalRepair(ctx context.Context, state *runLoopState, usage *provider.Usage, reasoningLen int) (bool, error) {
	a.sink.Emit(event.Event{
		Kind:   event.Notice,
		Level:  event.LevelInfo,
		Code:   event.NoticeCodeEmptyFinal,
		Text:   emptyFinalNotice(),
		Detail: emptyFinalNoticeDetail(a.prov.Name(), usage, reasoningLen),
	})
	a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(visibleFinalRepairMessage())})
	a.maybeCompact(ctx, usage)
	return true, nil
}

func visibleFinalRepairExhaustedError(state *runLoopState) error {
	return fmt.Errorf(
		"model finished without a visible final answer after %d finalization-only repair rounds",
		state.visibleFinalRepairRounds,
	)
}

func (a *Agent) handleVisibleFinalRepairToolRound(ctx context.Context, state *runLoopState, calls []provider.ToolCall, usage *provider.Usage, reasoningLen int) (bool, error) {
	a.blockVisibleFinalRepairTools(calls)
	if state.visibleFinalRepairRounds >= maxVisibleFinalRepairRounds {
		return false, visibleFinalRepairExhaustedError(state)
	}
	return a.requestVisibleFinalRepair(ctx, state, usage, reasoningLen)
}

// blockVisibleFinalRepairTools pairs every provider tool call without invoking
// the registry. A reasoning-only stop already declared the work complete; the
// bounded repair is allowed to render Content, never to repeat side effects.
func (a *Agent) blockVisibleFinalRepairTools(calls []provider.ToolCall) {
	const result = "blocked: visible-final repair is finalization-only; do not call tools or repeat work. Return concise visible answer text now."
	for _, call := range calls {
		a.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
			ID: call.ID, Name: call.Name, Args: call.Arguments,
		}})
		a.session.Add(provider.Message{
			Role: provider.RoleTool, Content: result, ToolCallID: call.ID, Name: call.Name,
		})
		a.sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
			ID: call.ID, Name: call.Name, Args: call.Arguments, Output: result,
			Err: result,
		}})
	}
}
