package agent

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func (a *Agent) emitIncompleteReadNotice(code, text, detail string) {
	if a == nil || a.svc.sink == nil {
		return
	}
	level := event.LevelWarn
	if code == event.NoticeCodeReadCompleted || code == event.NoticeCodeReadStrategyProgress || code == event.NoticeCodeReadStrategyResolved {
		level = event.LevelInfo
	}
	a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: level, Code: code, Text: text, Detail: detail})
}

// resolveIncompleteReadToolRoundBoundary runs after every tool result is stored,
// commits validated receipts, and counts at most one violation per model round.
func (a *Agent) resolveIncompleteReadToolRoundBoundary(ctx context.Context, state *turnRuntime, usage *provider.Usage) (cont bool, err error, handled bool) {
	round := state.incompleteReads.finishToolRound()
	for _, observed := range round.record {
		a.recordModelTextObservationValue(observed)
	}
	for _, readID := range round.resolvedIDs {
		a.emitIncompleteReadNotice(event.NoticeCodeReadStrategyResolved, "Reasonix validated a narrowed read strategy receipt.", "read_id="+readID)
	}
	if round.pause != nil {
		a.contextManager().ObserveUsage(usage)
		return false, round.pause, true
	}
	instruction := state.incompleteReads.nextInstruction()
	if instruction != "" {
		a.sess.conversation.Add(HostGeneratedUserMessage(a.withTurnPreferences(instruction)))
		a.emitIncompleteReadNotice(event.NoticeCodeReadContinuationRequired, "Reasonix is continuing or narrowing an incomplete file read before allowing changes or completion.", "read continuation instruction appended")
	}
	if ctx.Err() != nil {
		a.recordInterruptedDisplay("", "", nil, true, state.workDurationMs())
		return false, ctx.Err(), true
	}
	if instruction == "" {
		if len(round.resolvedIDs) > 0 {
			a.contextManager().ObserveUsage(usage)
			return true, nil, true
		}
		return false, nil, false
	}
	a.contextManager().ObserveUsage(usage)
	if axis, detail := a.task.budget.exceeded(a.taskBudgetLimit(ctx)); axis != "" {
		return false, &IncompleteReadError{Reason: fmt.Sprintf("the %s budget ended while a required read_file continuation was still pending: %s", axis, detail)}, true
	}
	return true, nil, true
}

func (a *Agent) boundIncompleteReadAwareResult(plan *toolCallPlan, result string) (body, truncMsg, original string, readObserver bool) {
	if plan == nil {
		return result, "", "", false
	}
	if plan.evidenceName == "read_file" {
		_, readObserver = plan.runTool.(tool.ModelTextObserver)
		if !readObserver {
			_, readObserver = plan.execTool.(tool.ModelTextObserver)
		}
	}
	body, truncMsg, original = a.boundProviderVisibleResult(result, plan.call.Name, plan.call.ID)
	return body, truncMsg, original, readObserver
}

func deferredIncompleteReadOutcome(plan *toolCallPlan, rawOutput string, readObserver, visibleFull bool) *incompleteReadDeferred {
	if plan == nil || !(readObserver || plan.evidenceName == "session_tool_result" || (plan.evidenceName == "grep" && plan.incompleteReadRoot != "")) {
		return nil
	}
	return &incompleteReadDeferred{plan: plan, rawOutput: rawOutput, readObserver: readObserver, visibleFull: visibleFull}
}

func readStrategyPreview(raw, readID string, totalTokens, limitTokens int) string {
	const maxPreview = 8 * 1024
	headLimit := min(len(raw), maxPreview-1024)
	head := snapToRuneBoundary(raw, 0, max(0, headLimit))
	if newline := strings.LastIndexByte(head, '\n'); newline >= 0 {
		head = head[:newline+1]
	}
	return fmt.Sprintf("%s\n[INCOMPLETE READ: read_id=%s; complete content does not fit the dynamic context budget (estimated_tokens=%d budget_tokens=%d). This prefix is not whole-file evidence. Follow the restricted grep/read strategy from the next host message.]\n", head, readID, totalTokens, limitTokens)
}

// finalizeIncompleteReadOutcome is called by executeBatch.finalize in provider
// order, never from parallel execution goroutines.
func (a *Agent) finalizeIncompleteReadOutcome(deferred *incompleteReadDeferred, out *toolOutcome) {
	if a == nil || deferred == nil || deferred.plan == nil || out == nil {
		return
	}
	plan := deferred.plan
	var transition incompleteReadTransition
	switch plan.evidenceName {
	case "read_file":
		observed, ok := modelTextObservationFor(plan, deferred.rawOutput)
		transition = a.turn.incompleteReads.observeReadFile(plan, deferred.rawOutput, out.output, observed, ok, a.estimatedReadResultTokens(deferred.rawOutput), a.readAutoRecoveryBudgetFor())
	case "session_tool_result":
		transition = a.turn.incompleteReads.observeResultPage(plan, deferred.rawOutput, a.retainedReadPageMatches(plan, deferred.rawOutput))
	case "grep":
		transition = a.turn.incompleteReads.observeStrategySearch(plan, deferred.rawOutput, deferred.visibleFull)
	default:
		return
	}
	for _, observed := range transition.record {
		a.recordModelTextObservationValue(observed)
	}
	if transition.strategyRequired {
		out.output = readStrategyPreview(deferred.rawOutput, transition.readID, transition.totalTokens, transition.limitTokens)
		out.rawOutput = deferred.rawOutput
		out.truncated = true
		out.truncMsg = fmt.Sprintf("read_file switched to restricted strategy: estimated_tokens=%d budget_tokens=%d", transition.totalTokens, transition.limitTokens)
		a.emitIncompleteReadNotice(event.NoticeCodeReadStrategyRequired, "The complete file did not fit the dynamic context budget; Reasonix entered restricted search/read mode.", fmt.Sprintf("read_id=%s bytes=%d tokens=%d budget_tokens=%d", transition.readID, transition.totalBytes, transition.totalTokens, transition.limitTokens))
	}
	if transition.detected {
		a.emitIncompleteReadNotice(event.NoticeCodeIncompleteReadDetected, "Reasonix detected an incomplete file read and armed a host continuation.", "read_id="+transition.readID)
	}
	if transition.strategyProgress {
		a.emitIncompleteReadNotice(event.NoticeCodeReadStrategyProgress, "Reasonix recorded valid progress in a restricted read strategy.", "read_id="+transition.readID)
	}
	if transition.localSafetyPaged {
		a.emitIncompleteReadNotice(event.NoticeCodeReadLocalSafetyPaged, "Reasonix safely paged a large local read_file result.", "read_id="+transition.readID)
	}
	if transition.completed {
		a.emitIncompleteReadNotice(event.NoticeCodeReadCompleted, "Reasonix finished recovering the complete file read.", "read_id="+transition.readID)
	}
}
