package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
)

// runLoopState holds per-Run loop counters and flags. It is package-private and
// not shared across goroutines; the first extraction keeps the existing lock
// model and only structures the sequential turn state machine.
type runLoopState struct {
	runMaxSteps       int
	runMaxStepsKey    string
	runLimitHostOwned bool

	finalReadinessBlocks int
	seenReadinessStates  map[string]struct{}
	emptyFinalBlocks     int
	handoffNudges        int
	usedAnyTool          bool
	streamRecoveries     int
	graceRound           bool
	recoveryGraceRound   bool

	todoProgress         int
	trackingTodoProgress bool
	todoStallRounds      int
	seenTodoProgress     map[string]struct{}

	executorHandoff bool
	// input is the user turn text after withTurnPreferences (used by handoff
	// nudges that inspect the original request wording).
	input string

	workDurationMs func() int64
}

// beginRunTurn handles evidence scope, delivery classification, background-job
// evidence re-lease, and the initial user-turn persistence. Callers still own
// all Run-level defers (workspace lease, evidence commit, delivery checkpoint,
// steer queue, active-turn timestamp).
func (a *Agent) beginRunTurn(ctx context.Context, input string) (rawInput string, state *runLoopState) {
	rawInput = RawUserInput(ctx, input)
	providerInput := input
	scope, scoped := DeliveryExecutionScopeFromContext(ctx)
	preserveEvidence := a.preserveEvidenceOnce
	if a.evidence != nil {
		switch {
		case preserveEvidence:
			a.evidence.ResetBackgroundLeases()
		case scoped && a.deliveryScopeID == scope.ID:
			a.evidence.ResetBackgroundLeases()
		default:
			a.evidence.Reset()
		}
	}
	a.preserveEvidenceOnce = false
	if !preserveEvidence {
		a.deliveryRecoveryPending = false
	}
	if scoped {
		a.deliveryScopeID = scope.ID
	} else if !preserveEvidence {
		a.deliveryScopeID = ""
	}
	a.deliveryScopeActive = scoped
	if scoped && a.deliveryCheckpoint.ScopeID != scope.ID {
		a.deliveryCheckpoint = evidence.DeliveryCheckpoint{ScopeID: scope.ID}
	}
	// Re-lease this session's background-job mutations that no turn has
	// committed yet. The Reset above just wiped any lease a failed or
	// cancelled turn held (its ledger is gone), and a process restart starts
	// from an empty ledger too — in both cases the job manager still marks the
	// job's evidence uncommitted. Without re-injecting it here, a turn that
	// never re-issues wait/bash_output (the model has no reason to if it
	// doesn't know a mutation is still pending) would ship the background
	// change without the final-readiness gate ever seeing it. Plan turns defer
	// this lease like collectBackgroundEvidence does so execution evidence is
	// consumed and audited only after plan approval.
	if a.evidence != nil && a.jobs != nil && !a.planMode.Load() {
		session := jobs.SessionFromContext(ctx)
		for _, jobID := range a.jobs.PendingEvidenceJobIDsForSession(session) {
			summary, ready := a.jobs.TryLeaseEvidenceForSession(session, jobID)
			if !ready {
				continue
			}
			if !a.evidence.NoteBackgroundLease(session, jobID) {
				continue
			}
			a.evidence.MergeChild(summary)
		}
	}
	a.deliveryCriteriaEstablished = a.hasIncompleteCanonicalCriteria() ||
		(a.evidence != nil && a.evidence.HasSuccessfulTodoWrite()) ||
		(scoped && a.deliveryCheckpoint.CriteriaEstablished)
	// Classify delivery expectations from the task text. Sub-agent spawners
	// pass the pristine task through Options.ClassifierTaskText (a trusted
	// host channel) because their Run input carries host framing whose
	// incidental verbs — "file tools resolve relative paths" — once classified
	// every workspace-wrapped subagent prompt as a mutation request and
	// deadlocked read-only subagents. Without the override the raw input is
	// classified verbatim: stripping user-controllable markup here would let
	// input dressed up as host framing disarm the delivery gates.
	classifierInput := a.classifierTaskText
	if scoped && strings.TrimSpace(scope.TaskText) != "" {
		classifierInput = scope.TaskText
	} else if strings.TrimSpace(classifierInput) == "" {
		classifierInput = rawInput
	}
	a.deliveryTaskExpected = deliveryTaskNeedsEvidence(classifierInput)
	a.deliveryMutationExpected = deliveryTaskNeedsMutation(classifierInput) && registryHasWriterTools(a.tools)
	a.recoveryTaskSummary = boundedRecoveryTaskSummary(classifierInput)
	// A cancelled/error turn leaves a provider-excluded recovery record at the
	// transcript tail. Fold its bounded facts into this new user turn exactly
	// once; the user's raw text remains the classifier source above.
	providerInput = withInterruptedRecovery(providerInput, a.pendingInterruptedRecovery())
	a.repeatSuccessCounts = nil
	if !scoped || a.repeatFailureScope != scope.ID {
		a.repeatFailureCounts = nil
	} else {
		// Only stale-anchor failures have a side-effect-free state recheck.
		// Ordinary write failures may recover between Runs after user action or
		// an external state change, so do not carry their retry budget forward.
		for sig, failure := range a.repeatFailureCounts {
			if !failure.stateRecheck {
				delete(a.repeatFailureCounts, sig)
			}
		}
	}
	if scoped {
		a.repeatFailureScope = scope.ID
	} else {
		a.repeatFailureScope = ""
	}
	a.blockedTurnStreak = 0
	a.loopGuardArmed = false
	a.loopGuardReceiptMark = 0
	a.sink.Emit(event.Event{Kind: event.TurnStarted})
	input = a.withTurnPreferences(providerInput)
	userCreatedAt := time.Now().UnixMilli()
	a.activeTurnCreatedAt.Store(userCreatedAt)
	rawContent := ""
	if input != rawInput {
		rawContent = rawInput
	}
	a.session.Add(provider.Message{
		Role: provider.RoleUser, Content: input, RawContent: rawContent,
		Images: userImages(ctx), CreatedAt: userCreatedAt,
	})

	state = &runLoopState{
		finalReadinessBlocks: 0,
		seenReadinessStates:  make(map[string]struct{}),
		emptyFinalBlocks:     0,
		handoffNudges:        0,
		usedAnyTool:          false,
		streamRecoveries:     0,
		graceRound:           false,
		recoveryGraceRound:   false,
		todoStallRounds:      0,
		seenTodoProgress:     make(map[string]struct{}),
		executorHandoff:      a.executorHandoffGuard && strings.Contains(input, executorHandoffMarker),
		input:                input,
	}
	state.todoProgress, state.trackingTodoProgress = a.canonicalTodoProgress()
	if a.evidence != nil {
		for _, sig := range a.evidence.SuccessfulProgressSignaturesSince(0) {
			state.seenTodoProgress[sig] = struct{}{}
		}
	}
	return rawInput, state
}

// runToolLoop owns the main tool-round budget and dispatches each streamed
// assistant turn into final-response or tool-round handling.
func (a *Agent) runToolLoop(ctx context.Context, state *runLoopState) error {
	for step := 0; state.runMaxSteps <= 0 || step < state.runMaxSteps || state.graceRound || state.recoveryGraceRound; step++ {
		// Consume a queued steer and persist it to the session so it
		// survives tab switches and history replay. The model sees it as
		// guidance (with a prefix), not a new task. One cache miss per
		// steer is unavoidable — the model must see the new instruction.
		if text, ok := a.consumeSteer(); ok {
			a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(midTurnSteerMessage(text))})
			a.sink.Emit(event.Event{Kind: event.Steer, Text: text})
		}
		schemas := a.tools.Schemas()
		prefixShape := a.capturePrefixShape(schemas)
		prevPrefixShape := a.lastPrefixShape
		if !a.haveLastPrefixShape {
			prevPrefixShape = prefixShape
		}

		text, reasoning, signature, calls, usage, interrupted, partialToolStarted, partialCalls, err := a.stream(ctx, step+1)
		if err != nil {
			if interrupted && state.streamRecoveries < maxStreamRecoveries {
				state.streamRecoveries++
				a.recordInterruptedDisplay(text, reasoning, partialCalls, false, state.workDurationMs())
				a.session.Add(provider.Message{
					Role:    provider.RoleUser,
					Content: a.withTurnPreferences(streamRecoveryMessage(hasVisibleFinalAnswer(text), partialToolStarted)),
				})
				a.sink.Emit(event.Event{Kind: event.Retrying, RetryAttempt: state.streamRecoveries, RetryMax: maxStreamRecoveries})
				step-- // recovery retries do not consume the tool-round maxSteps budget
				continue
			}
			a.recordInterruptedDisplay(text, reasoning, partialCalls, true, state.workDurationMs())
			return err
		}
		state.streamRecoveries = 0
		cacheDiagnostics := CompareShape(prevPrefixShape, prefixShape, usage)
		a.lastPrefixShape = prefixShape
		a.haveLastPrefixShape = true
		if usage != nil && usage.TotalTokens > 0 {
			a.sink.Emit(event.Event{Kind: event.Usage, Usage: usage, Pricing: a.pricing,
				UsageSource:      a.usageSource,
				CacheDiagnostics: &cacheDiagnostics,
				SessionHit:       int(a.sessCacheHit.Load()), SessionMiss: int(a.sessCacheMiss.Load())})
		}
		if msg, ok := finishReasonMessage(usage); ok {
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: msg})
		}

		// Keep reasoning_content on the assistant turn for display and session
		// archive. Most OpenAI-compatible backends do not replay it; providers
		// with an explicit round-trip contract retain the raw provider text.
		calls = a.withPreviewFileDiffs(calls)
		a.warnMissingToolCallReasoning(calls, reasoning)
		a.session.Add(provider.Message{
			Role:               provider.RoleAssistant,
			Content:            text,
			ReasoningContent:   reasoning,
			ReasoningSignature: signature,
			ToolCalls:          calls,
			WorkDurationMs:     state.workDurationMs(),
		})

		if len(calls) == 0 {
			cont, ferr := a.handleFinalResponse(ctx, state, text, reasoning, usage)
			if !cont {
				return ferr
			}
			continue
		}

		cont, terr := a.handleToolRound(ctx, state, step, calls, usage)
		if !cont {
			return terr
		}
	}
	// Only reached when a positive maxSteps guard is configured. The work so far
	// is already in the session, so the user can just send another message to pick
	// up where it left off.
	return &maxStepsPause{steps: state.runMaxSteps, key: state.runMaxStepsKey}
}

// handleFinalResponse processes a no-tool assistant turn: recovery pause,
// readiness retry, empty final retry, executor handoff nudge, steer drain, and
// final compaction. cont=true continues the tool loop; cont=false returns err
// from Run (err may be nil for a clean final answer).
func (a *Agent) handleFinalResponse(ctx context.Context, state *runLoopState, text, reasoning string, usage *provider.Usage) (cont bool, err error) {
	// Recovery finalization produced a summary. Keep it in the session,
	// but still pause so Goal auto-continue cannot open another Run with
	// a fresh finalization round. turn_done reports recovery_paused.
	if state.recoveryGraceRound {
		a.maybeCompact(ctx, usage)
		reason := ""
		if ctrl := a.recoveryEpisodeControl(); ctrl != nil {
			_, _ = ctrl.ConsumeFinalization(a.recoveryTaskID)
		}
		return false, &RecoveryPauseError{
			Message:    "Automatic retries paused. Reasonix stopped repeated attempts and kept completed work. Send \"continue\" to start a fresh attempt, or add instructions to change direction.",
			StopReason: reason,
		}
	}
	finalizeTask := !a.deliveryScopeActive || deliveryDisposition(text) == deliveryGoalFinal
	readiness := a.finalReadinessCheckFor(finalizeTask)
	if state.graceRound && (readiness.reason != "" || !hasVisibleFinalAnswer(text)) {
		a.maybeCompact(ctx, usage)
		return false, &maxStepsPause{steps: state.runMaxSteps, key: state.runMaxStepsKey}
	}
	if readiness.reason != "" {
		// Extend the base retry budget only when the missing-requirement
		// state actually changes. Counting ledger length let repeated reads,
		// failed bookkeeping, or other irrelevant receipts masquerade as
		// convergence and burn through six expensive model calls.
		sig := readiness.progressSignature()
		_, repeatedState := state.seenReadinessStates[sig]
		progressed := state.finalReadinessBlocks > 0 && !repeatedState
		state.seenReadinessStates[sig] = struct{}{}
		state.finalReadinessBlocks++
		exhausted := state.finalReadinessBlocks >= maxFinalReadinessBlocksWithProgress ||
			(state.finalReadinessBlocks >= maxFinalReadinessBlocks && !progressed)
		result := evidence.ReadinessBlocked
		if exhausted {
			result = evidence.ReadinessErrored
			event.RecordReadinessAudit(a.sink, readiness.audit(result, false))
			a.deliveryRecoveryPending = true
			return false, &FinalReadinessError{Attempts: state.finalReadinessBlocks, Reason: readiness.reason, Missing: readiness.missingIDs()}
		}
		event.RecordReadinessAudit(a.sink, readiness.audit(result, false))
		a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeFinalReadiness, Text: finalReadinessNoticeText(), Detail: readiness.reason})
		a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(finalReadinessRetryMessage(readiness.reason))})
		a.maybeCompact(ctx, usage)
		return true, nil
	}
	if !hasVisibleFinalAnswer(text) {
		// DeepSeek thinking mode can stream a long reasoning_content and
		// then finish with finish_reason="stop" but an empty content
		// block: the model has explicitly signalled completion and its
		// reasoning was already streamed to the user. Retrying here overrides
		// that stop signal and forces another expensive thinking round (the
		// "still thinking after the task is done" symptom), so honour the
		// stop when reasoning carried the substance of the answer and treat
		// the turn as a final answer instead of retrying.
		if !reasoningOnlyFinishHonoured(a.prov, usage, reasoning) {
			state.emptyFinalBlocks++
			if state.emptyFinalBlocks >= maxEmptyFinalBlocks {
				return false, fmt.Errorf("model finished without a visible final answer %d times", state.emptyFinalBlocks)
			}
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeEmptyFinal, Text: emptyFinalNotice(), Detail: emptyFinalNoticeDetail(a.prov.Name(), usage, len(reasoning))})
			a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(emptyFinalRetryMessage())})
			a.maybeCompact(ctx, usage)
			return true, nil
		}
	}
	if state.executorHandoff && !state.usedAnyTool && state.handoffNudges < maxExecutorHandoffNudges && shouldNudgeExecutorHandoff(state.input, text) {
		state.handoffNudges++
		a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeExecutorHandoff, Text: executorHandoffNoticeText(), Detail: "executor answered without taking any action; nudging it to use its tools"})
		a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(executorHandoffRetryMessage())})
		a.maybeCompact(ctx, usage)
		return true, nil
	}
	if readiness.applies {
		event.RecordReadinessAudit(a.sink, readiness.audit(evidence.ReadinessAllowed, state.finalReadinessBlocks > 0))
	}
	if a.steerQueueLen() > 0 {
		return true, nil
	}
	// A final-answer turn otherwise skips compaction, so a large context
	// carries into the next turn un-folded and can overflow the model window.
	// No-op below the trigger, so normal turns keep their warm cache.
	a.maybeCompact(ctx, usage)
	return false, nil // model gave a final answer
}

// handleToolRound executes a tool batch, persists tool messages, handles
// cancellation, todo stall tracking, recovery finalization pause, and the
// max-steps grace round. cont=true continues the tool loop; cont=false returns
// err from Run.
func (a *Agent) handleToolRound(ctx context.Context, state *runLoopState, step int, calls []provider.ToolCall, usage *provider.Usage) (cont bool, err error) {
	state.emptyFinalBlocks = 0
	state.usedAnyTool = true

	// Grace round guard: if we already gave the model one extra response
	// and it still wants to call tools, stop here.
	if state.graceRound {
		return false, &maxStepsPause{steps: state.runMaxSteps, key: state.runMaxStepsKey}
	}
	// Recovery Episode exhausted: one finalization round only. Further tool
	// calls are not executed; return a typed pause so the host can surface
	// recovery_paused without treating it as a send failure.
	if state.recoveryGraceRound {
		reason := ""
		if ctrl := a.recoveryEpisodeControl(); ctrl != nil {
			_, _ = ctrl.ConsumeFinalization(a.recoveryTaskID)
		}
		// Pair tool-call / tool-result without executing.
		msg := "blocked: Auto recovery already paused this turn. Do not call tools; the user will continue in the next message."
		for _, call := range calls {
			a.session.Add(provider.Message{
				Role:       provider.RoleTool,
				Content:    msg,
				ToolCallID: call.ID,
				Name:       call.Name,
			})
		}
		a.maybeCompact(ctx, usage)
		return false, &RecoveryPauseError{
			Message:    "Automatic retries paused. Reasonix stopped repeated attempts and kept completed work. Send \"continue\" to start a fresh attempt, or add instructions to change direction.",
			StopReason: reason,
		}
	}

	receiptMark := 0
	if a.evidence != nil {
		receiptMark = a.evidence.Len()
	}
	batch := a.executeBatch(ctx, calls)
	results, images := batch.results, batch.images
	for i, call := range calls {
		a.session.Add(provider.Message{
			Role:       provider.RoleTool,
			Content:    results[i],
			Images:     images[i],
			ToolCallID: call.ID,
			Name:       call.Name,
		})
	}
	// If the context was cancelled during tool execution, return after storing
	// the batch results so the session keeps paired tool-call history.
	if ctx.Err() != nil {
		a.recordInterruptedDisplay("", "", nil, true, state.workDurationMs())
		return false, ctx.Err()
	}
	if !a.planMode.Load() {
		nextProgress, nextTracking := a.canonicalTodoProgress()
		hostProgress := false
		if a.evidence != nil {
			for _, sig := range a.evidence.SuccessfulProgressSignaturesSince(receiptMark) {
				if _, seen := state.seenTodoProgress[sig]; !seen {
					hostProgress = true
					state.seenTodoProgress[sig] = struct{}{}
				}
			}
		}
		switch {
		case !nextTracking:
			state.todoStallRounds = 0
		case !state.trackingTodoProgress || nextProgress > state.todoProgress || hostProgress:
			state.todoStallRounds = 0
		default:
			state.todoStallRounds++
		}
		state.todoProgress, state.trackingTodoProgress = nextProgress, nextTracking
		if state.todoStallRounds == todoProgressNudgeRounds {
			nudge := todoProgressNudgeMessage(state.todoStallRounds)
			a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(nudge)})
			a.sink.Emit(event.Event{
				Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeLoopGuard,
				Text:   loopGuardNoticeText(),
				Detail: fmt.Sprintf("the current todo has no new completion, unique read, command, or mutation for %d consecutive tool-call rounds; asking the assistant to reassess", state.todoStallRounds),
			})
		}
		if state.todoStallRounds >= maxTodoStallRounds {
			a.sink.Emit(event.Event{
				Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeLoopGuard,
				Text:   "Task progress stalled; pausing before more tools are called.",
				Detail: fmt.Sprintf("the current todo has no new completion, unique read, command, or mutation for %d consecutive tool-call rounds after a host reassessment; work is saved and can be resumed", state.todoStallRounds),
			})
			return false, &todoStallPause{rounds: state.todoStallRounds}
		}
	}

	// The prompt only grows from here; compact before the next turn so it
	// stays within the model's window.
	a.maybeCompact(ctx, usage)

	// When Auto recovery exhausts its Episode budget, offer exactly one
	// summarize-only finalization round. Successful summary ends cleanly;
	// further tool calls surface RecoveryPauseError.
	if batch.recoveryStopTurn && !state.recoveryGraceRound {
		state.recoveryGraceRound = true
		if ctrl := a.recoveryEpisodeControl(); ctrl != nil {
			ctrl.MarkFinalizationOffered(a.recoveryTaskID)
		}
		nudge := "Auto recovery has reached its limit for this turn. Do not call any more tools. Summarize what was completed, what failed, and what the user should do next. The user can continue in the next message."
		a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(nudge)})
		return true, nil
	}

	// When the tool-call budget runs out this round, give the model
	// one grace round to produce a final answer from completed work.
	if state.runMaxSteps > 0 && step+1 >= state.runMaxSteps {
		state.graceRound = true
		nextStep := fmt.Sprintf("The user can increase %s or continue in the next turn if more work is needed.", state.runMaxStepsKey)
		if state.runLimitHostOwned {
			nextStep = "Use the evidence already collected, label remaining uncertainty, and keep the final answer actionable."
		}
		nudge := fmt.Sprintf("Do not call any more tools — your tool-call round limit (%s) has been reached. Instead, synthesize a final answer from all the work already completed: summarize what was accomplished, what remains to be done, and any decisions the user should make. %s", state.runMaxStepsKey, nextStep)
		a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(nudge)})
		a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeToolBudget, Text: toolBudgetNoticeText(), Detail: fmt.Sprintf("budget (%s=%d) exhausted: one grace round to finalize", state.runMaxStepsKey, state.runMaxSteps)})
	}
	return true, nil
}
