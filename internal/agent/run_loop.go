package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/runtimepolicy"
	"reasonix/internal/tool"
)

// streamedTurn is one provider completion collected by stream. Keeping the
// result together makes the missing-reasoning recovery path explicit: the
// first, malformed completion is never committed before a safe replacement is
// available, and a failed recovery can still fall back to the complete first
// response without re-running any tool.
type streamedTurn struct {
	text               string
	reasoning          string
	signature          string
	reasoningID        string
	reasoningStatus    string
	reasoningComplete  bool
	calls              []provider.ToolCall
	responsesItems     []json.RawMessage
	serverSearch       []provider.ServerSearchCall
	usage              *provider.Usage
	interrupted        bool
	partialToolStarted bool
	partialCalls       []provider.ToolCall
	maxArgChars        int // peak streaming tool-arg size for failed-attempt estimates
	err                error
}

func (s streamedTurn) assistantMessage() provider.Message {
	return provider.Message{
		Role: provider.RoleAssistant, Content: s.text, ReasoningContent: s.reasoning,
		ReasoningSignature: s.signature, ReasoningID: s.reasoningID, ReasoningStatus: s.reasoningStatus,
		ToolCalls: s.calls, ResponsesItems: s.responsesItems, ServerSearch: s.serverSearch,
	}
}

// deferredStreamSink keeps selected stream events local until the caller
// chooses which provider response to adopt. On an ordinary healthy DeepSeek
// turn, reasoning arrives before tool calls and unlocks live tool-card events.
// On the rare malformed turn with no reasoning, only the speculative partial
// tool cards remain buffered, so retrying does not flash duplicate cards in the
// UI. A recovery attempt buffers everything because it may be discarded.
type deferredStreamSink struct {
	inner               event.Sink
	deferAll            bool
	waitingForReasoning bool
	sawReasoning        bool
	events              []event.Event
}

func newReasoningAwareStreamSink(inner event.Sink) *deferredStreamSink {
	return &deferredStreamSink{inner: inner, waitingForReasoning: true}
}

func newDeferredStreamSink(inner event.Sink) *deferredStreamSink {
	return &deferredStreamSink{inner: inner, deferAll: true}
}

func (s *deferredStreamSink) Emit(e event.Event) {
	if s == nil {
		return
	}
	if s.deferAll {
		s.events = append(s.events, e)
		return
	}
	if s.waitingForReasoning && e.Kind == event.Reasoning && strings.TrimSpace(e.Text) != "" {
		s.sawReasoning = true
		s.inner.Emit(e)
		s.flushBuffered()
		return
	}
	if s.waitingForReasoning && !s.sawReasoning {
		switch e.Kind {
		case event.ToolDispatch, event.ToolResult, event.Text, event.Message:
			// Keep every user-visible speculative event private until reasoning
			// proves the turn replayable. Healthy DeepSeek responses emit
			// reasoning first, so their live-streaming fast path is unchanged.
			s.events = append(s.events, e)
			return
		}
	}
	s.inner.Emit(e)
}

func (s *deferredStreamSink) flushBuffered() {
	if s == nil {
		return
	}
	for _, e := range s.events {
		s.inner.Emit(e)
	}
	s.events = nil
}

func (s *deferredStreamSink) Flush() {
	if s == nil {
		return
	}
	s.flushBuffered()
}

func (s *deferredStreamSink) Discard() {
	if s != nil {
		s.events = nil
	}
}

// beginRunTurn handles evidence scope, delivery classification, background-job
// evidence re-lease, and the initial user-turn persistence. Callers still own
// all Run-level defers (workspace lease, evidence commit, delivery checkpoint,
// steer queue, active-turn timestamp).
func (a *Agent) beginRunTurn(ctx context.Context, input string) (rawInput string, state *turnRuntime) {
	rawInput = RawUserInput(ctx, input)
	providerInput := input
	// A fresh user turn starts from zeroed per-turn host state; the new turn's
	// values are computed below. Cross-turn state (checkpoint, scope, failure
	// budgets) lives in taskRuntime and is reconciled there.
	a.turn = turnRuntime{}
	a.resetStructuralRunGuards()
	scope, scoped := DeliveryExecutionScopeFromContext(ctx)
	preserveEvidence, readinessRecovered := a.beginFinalReadinessRecovery()
	a.turn.readinessRecovered = readinessRecovered
	if a.task.ledger != nil {
		switch {
		case preserveEvidence:
			a.task.ledger.ResetBackgroundLeases()
		case scoped && a.task.scopeID == scope.ID:
			a.task.ledger.ResetBackgroundLeases()
		default:
			a.resetTurnEvidence()
		}
	}
	if scoped {
		a.task.scopeID = scope.ID
	} else if !preserveEvidence {
		a.task.scopeID = ""
	}
	a.turn.deliveryScopeActive = scoped
	if scoped && a.task.checkpoint.ScopeID != scope.ID {
		a.task.checkpoint = evidence.DeliveryCheckpoint{ScopeID: scope.ID}
	}
	a.leasePendingBackgroundEvidence(ctx)
	a.turn.deliveryCriteriaEstablished = a.hasIncompleteCanonicalCriteria() ||
		(a.task.ledger != nil && a.task.ledger.HasSuccessfulTodoWrite()) ||
		(scoped && a.task.checkpoint.CriteriaEstablished)
	// Classify delivery expectations from the task text. Sub-agent spawners
	// pass the pristine task through Options.ClassifierTaskText (a trusted
	// host channel) because their Run input carries host framing whose
	// incidental verbs — "file tools resolve relative paths" — once classified
	// every workspace-wrapped subagent prompt as a mutation request and
	// deadlocked read-only subagents. Without the override the raw input is
	// classified verbatim: stripping user-controllable markup here would let
	// input dressed up as host framing disarm the delivery gates.
	a.turn.turnInput = a.classifierTaskText
	if scoped && strings.TrimSpace(scope.TaskText) != "" {
		a.turn.turnInput = scope.TaskText
	} else if strings.TrimSpace(a.turn.turnInput) == "" {
		a.turn.turnInput = rawInput
	}
	a.turn.recoveryTaskSummary = boundedRecoveryTaskSummary(a.turn.turnInput)
	if constraints, ok := runtimepolicy.FromContext(ctx); ok {
		a.turn.constraints = constraints
	} else {
		a.turn.constraints = runtimepolicy.ParseConstraints(runtimepolicy.StripQuotedConstraints(a.turn.turnInput))
		if a.planMode.Load() {
			a.turn.constraints.PlanModeReadOnly = true
			a.turn.constraints.ForbidMutation = true
		}
	}
	if inherited, ok := runtimepolicy.InheritedFromContext(ctx); ok && !a.readOnlyExecution {
		a.turn.constraints = mergeInheritedConstraints(a.turn.constraints, inherited.Constraints)
		if inherited.PlanReadOnly {
			a.turn.constraints.PlanModeReadOnly = true
			a.turn.constraints.ForbidMutation = true
		}
	} else if a.inheritedExec != nil && !a.readOnlyExecution {
		a.turn.constraints = mergeInheritedConstraints(a.turn.constraints, a.inheritedExec.Constraints)
		if a.inheritedExec.PlanReadOnly {
			a.turn.constraints.PlanModeReadOnly = true
			a.turn.constraints.ForbidMutation = true
		}
	}
	a.turn.engine = runtimepolicy.NewEngine(a.turn.constraints)
	a.rebuildTurnContract()
	// Reuse an open provider/configuration circuit before projecting history or
	// spending another pair of normal thinking-mode requests.
	if a.beginMissingReasoningRecovery() {
		event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetrySuppressed})
		event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningFallback})
	}
	// A cancelled/error turn leaves a provider-excluded recovery record at the
	// transcript tail. Fold its bounded facts into this new user turn exactly
	// once; the user's raw text remains the source above.
	a.ensureUnreplayableHistoryRecovery()
	providerInput = withInterruptedRecovery(providerInput, a.pendingInterruptedRecovery())
	a.task.prepareScope(scoped, scope.ID)
	a.svc.sink.Emit(event.Event{Kind: event.TurnStarted})
	a.emitTurnPhase(event.TurnPhaseWorking)
	input = a.withTurnPreferences(providerInput)
	userCreatedAt := time.Now().UnixMilli()
	a.activeTurnCreatedAt.Store(userCreatedAt)
	rawContent := rawInput
	if rawContent == "" {
		rawContent = a.turn.turnInput
	}
	a.sess.conversation.Add(provider.Message{
		Role: provider.RoleUser, Content: input, RawContent: rawContent,
		Images: userImages(ctx), VisionSummary: VisionSummaryFromContext(ctx), CreatedAt: userCreatedAt,
	})

	// The loop fields join the classification computed above rather than
	// opening a second object: one turn, one turnRuntime. The zero values the
	// old literal spelled out are already there from the reset at the top.
	state = &a.turn
	state.seenTodoProgress = make(map[string]struct{})
	state.executorHandoff = a.executorHandoffGuard && strings.Contains(input, executorHandoffMarker)
	state.input = input
	state.budget = runBudget{started: time.Now()}
	state.todoProgress, state.trackingTodoProgress = a.canonicalTodoProgress()
	if a.task.ledger != nil {
		for _, sig := range a.task.ledger.SuccessfulProgressSignaturesSince(0) {
			state.seenTodoProgress[sig] = struct{}{}
		}
	}
	return rawInput, state
}

// runToolLoop owns the main tool-round budget and dispatches each streamed
// assistant turn into final-response or tool-round handling.
func (a *Agent) runToolLoop(ctx context.Context, state *turnRuntime) error {
	ctx = a.withAgentContext(ctx)
	for step := 0; state.runMaxSteps <= 0 || step < state.runMaxSteps || state.graceRound || state.recoveryGraceRound; step++ {
		// Consume a queued steer and persist it to the session so it
		// survives tab switches and history replay. The model sees it as
		// guidance (with a prefix), not a new task. One cache miss per
		// steer is unavoidable — the model must see the new instruction.
		if text, itemID, ok := a.consumeSteer(); ok {
			a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(midTurnSteerMessage(text))})
			a.svc.sink.Emit(event.Event{Kind: event.Steer, Text: text, ItemID: itemID})
		} else if itemID != "" {
			// Loader failed after dequeue: durable entry stays for inspection
			// (unapplied path marks uncertain + pause via the notice sink).
			a.RecordUnappliedSteer("(body load failed)", itemID)
		}
		schemas := a.providerToolSchemas()
		prefixShape := a.capturePrefixShape(schemas)
		prevPrefixShape := a.sess.lastPrefixShape
		if !a.sess.haveLastPrefixShape {
			prevPrefixShape = prefixShape
		}

		// Drain reasons queued since the previous capture (compaction,
		// snip/prune, rewind, guardian merge) so CompareShape can attribute
		// any prefix change to the operation that actually caused it, instead
		// of a generic rewrite signal that also fires on local-only metadata
		// edits.
		contentReasons := a.sess.conversation.DrainContentRewriteReasons()

		// Prefix shape is captured once before sampling and frozen for the
		// whole attempt lifecycle — stream retries must not rewrite session
		// history mid-round, so the shape stays stable across body replays.
		streamed := a.streamWithSamplingRecovery(ctx, step+1)
		text, reasoning, signature, calls, responsesItems, serverSearch, usage := streamed.text, streamed.reasoning, streamed.signature, streamed.calls, streamed.responsesItems, streamed.serverSearch, streamed.usage
		partialCalls, err := streamed.partialCalls, streamed.err
		cacheDiagnostics := CompareShape(prevPrefixShape, prefixShape, usage, contentReasons)
		if err != nil {
			quote := a.emitTurnUsage(usage, &cacheDiagnostics)
			a.observeRunBudget(state, usage, quote)
			if msg, ok := finishReasonMessage(usage); ok {
				a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: msg})
			}
			// Exhausted stream retries (or a non-retryable error): persist one
			// bounded LocalOnly recovery record for the next real user message.
			// Intermediate failed attempts never wrote session state.
			a.recordInterruptedDisplay(text, reasoning, partialCalls, true, state.workDurationMs())
			return err
		}
		a.sess.lastPrefixShape = prefixShape
		a.sess.haveLastPrefixShape = true
		quote := a.emitTurnUsage(usage, &cacheDiagnostics)
		a.observeRunBudget(state, usage, quote)
		if msg, ok := finishReasonMessage(usage); ok {
			a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: msg})
		}

		// Commit boundary: only a clean terminal attempt reaches here.
		// Keep reasoning_content on the assistant turn for display and session
		// archive. Most OpenAI-compatible backends do not replay it; providers
		// with an explicit round-trip contract retain the raw provider text.
		calls = a.withPreviewFileDiffs(ctx, calls)
		a.sess.conversation.Add(provider.Message{
			Role:               provider.RoleAssistant,
			Content:            text,
			ReasoningContent:   reasoning,
			ReasoningSignature: signature,
			ReasoningID:        streamed.reasoningID,
			ReasoningStatus:    streamed.reasoningStatus,
			ToolCalls:          calls,
			ResponsesItems:     responsesItems,
			ServerSearch:       serverSearch,
			WorkDurationMs:     state.workDurationMs(),
		})

		if len(calls) == 0 {
			cont, ferr := a.handleFinalResponse(ctx, state, text, reasoning, usage)
			if !cont {
				return ferr
			}
			continue
		}

		// Invariant: executeBatch only ever receives tool calls from a
		// committed sampling attempt (clean terminal + response intercept).
		cont, terr := a.handleToolRound(ctx, state, step, text, reasoning, calls, usage)
		if !cont {
			return terr
		}
	}
	// Only reached when a positive maxSteps guard is configured. The work so far
	// is already in the session, so the user can just send another message to pick
	// up where it left off.
	return a.gracePause(state)
}

// streamWithSamplingRecovery coordinates Codex-style original-request replay
// for one model round: prepare once, freeze the provider request, run up to
// maxSamplingAttempts body attempts, and only commit after a clean terminal.
// Failed attempts never write Session state or execute tools. missing-reasoning
// repair shares this lifecycle (at most one extra exact replay).
func (a *Agent) streamWithSamplingRecovery(ctx context.Context, turn int) streamedTurn {
	frozen, err := a.prepareSamplingRequest(ctx)
	if err != nil {
		return streamedTurn{err: err}
	}
	// One request counter spans every body attempt; each attempt records only
	// its delta so RequestCount equals real HTTP POSTs (no triangular growth).
	ctx = provider.WithRequestAttemptCounter(ctx)
	var contextRecovery contextRecoveryBudget

	var billable *provider.Usage
	var last streamedTurn

	runAttempt := func(attemptID string, sink event.Sink) streamedTurn {
		return a.runSamplingAttempt(ctx, turn, sink, &frozen, attemptID)
	}

	for attempt := 1; attempt <= maxSamplingAttempts; attempt++ {
		attemptID := newStreamAttemptID(attempt)
		a.emitStreamAttempt(attemptID, event.StreamAttemptBegin, attempt, "", nil)

		streamSink, attemptSink := a.samplingAttemptSinks()

		result := runAttempt(attemptID, attemptSink)
		billable, last = a.recordSamplingAttempt(billable, result)

		if result.err != nil {
			if next, retryContext, _ := a.recoverContextLimit(ctx, frozen, result.err, &contextRecovery); retryContext {
				if streamSink != nil {
					streamSink.Discard()
				}
				a.emitStreamAttempt(attemptID, event.StreamAttemptDiscard, attempt, "context_limit", result.err)
				frozen = next
				attempt = 0
				continue
			}
			retry, terminal := a.handleSamplingError(ctx, attemptID, attempt, streamSink, &frozen, result, last, billable)
			if retry {
				continue
			}
			if provider.AsContextLimitError(result.err) != nil {
				a.setLastRecovery(contextRecoveryFailed)
			}
			return terminal
		}

		// Clean terminal. Repair missing replay-required reasoning with one exact
		// replay of the same frozen request (no synthetic prompt). A visible text
		// prefix does not make a tool turn replayable.
		issue := a.reasoningReplayIssue(result)
		missing, shouldRetry := false, false
		switch issue {
		case ReasoningReplayMissing:
			missing = true
			_, shouldRetry = a.observeMissingAssistantReasoning(result.assistantMessage(), result.reasoningComplete)
		case "":
			// Healthy replay-required turns advance the persisted anti-flapping
			// streak and eventually re-arm recovery for a future regression.
			a.observeMissingAssistantReasoning(result.assistantMessage(), result.reasoningComplete)
		}
		if issue == ReasoningReplayOverflow {
			event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryReasoningOverflowDetected})
			result.usage = finalizeSamplingUsage(billable, result.usage)
			terminal := a.finishUnreplayableReasoning(result, streamSink, issue)
			a.emitReasoningReplayAttemptOutcome(attemptID, attempt, terminal.err)
			return terminal
		}
		if missing {
			event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningDetected})
			if shouldRetry {
				event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetryAttempted})
				a.emitProtocolRetry(1, provider.SupportsMissingReasoningFallback(a.svc.prov))
				retrySink := newDeferredStreamSink(a.svc.sink)
				retry := runAttempt(attemptID, retrySink)
				billable = mergeSamplingUsage(billable, retry.usage)
				if retry.err != nil {
					retrySink.Discard()
					if ctx.Err() != nil {
						streamSink.Discard()
						a.emitStreamAttempt(attemptID, event.StreamAttemptDiscard, attempt, provider.StreamInterruptReason(retry.err), retry.err)
						// Use the cancelled retry as the "latest" shape so
						// FinishReason=interrupted is preserved for accounting.
						return streamedTurn{usage: finalizeSamplingUsage(billable, retry.usage), err: retry.err}
					}
					// Classify the first complete response without executing an
					// unreplayable client tool.
					a.storeLatestRequestUsage(result.usage)
					result.usage = finalizeSamplingUsage(billable, result.usage)
					event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningFallback})
					terminal := a.finishUnreplayableReasoning(result, streamSink, issue)
					a.emitReasoningReplayAttemptOutcome(attemptID, attempt, terminal.err)
					return terminal
				}
				streamSink.Discard()
				if a.reasoningReplayIssue(retry) == ReasoningReplayMissing {
					event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningDetected})
					if fallback, ok := a.runMissingReasoningFallback(ctx, turn, &frozen, attemptID, attempt, billable, retrySink); ok {
						return fallback
					}
				}
				retry = a.finishReasoningReplayRetry(retry, retrySink, billable)
				a.emitReasoningReplayAttemptOutcome(attemptID, attempt, retry.err)
				return retry
			}
			if !shouldRetry {
				event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetrySuppressed})
				if fallback, ok := a.runMissingReasoningFallback(ctx, turn, &frozen, attemptID, attempt, billable, streamSink); ok {
					return fallback
				}
				event.RecordProtocolRecovery(a.svc.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningFallback})
				result.usage = finalizeSamplingUsage(billable, result.usage)
				terminal := a.finishUnreplayableReasoning(result, streamSink, issue)
				a.emitReasoningReplayAttemptOutcome(attemptID, attempt, terminal.err)
				return terminal
			}
		}

		streamSink.Flush()
		a.emitStreamAttempt(attemptID, event.StreamAttemptCommit, attempt, "", nil)
		result.usage = finalizeSamplingUsage(billable, result.usage)
		return result
	}
	return last
}

func (a *Agent) emitProtocolRetry(attempt int, hasFallback bool) {
	maxAttempts := 1
	if hasFallback {
		maxAttempts = 2
	}
	a.svc.sink.Emit(event.Event{
		Kind: event.Retrying, RetryAttempt: attempt, RetryMax: maxAttempts,
		RetryScope: event.RetryScopeProtocol,
	})
}

func (a *Agent) emitStreamAttempt(id string, action event.StreamAttemptAction, attempt int, reason string, err error) {
	if reason == "" && err != nil {
		reason = provider.StreamInterruptReason(err)
	}
	a.svc.sink.Emit(event.Event{
		Kind: event.StreamAttempt,
		StreamAttempt: event.StreamAttemptInfo{
			ID: id, Action: action, Attempt: attempt, Max: maxSamplingAttempts, Reason: reason,
		},
	})
}

func newStreamAttemptID(attempt int) string {
	// Host-local only: never persisted, never sent to the model.
	return fmt.Sprintf("sa-%d-%d", attempt, time.Now().UnixNano())
}

// streamRetrySleep is the body-retry backoff. Tests replace it with a no-op so
// recovery suites stay fast while production keeps the Codex-shaped delays.
var streamRetrySleep = sleepStreamRetryBackoff

// sleepStreamRetryBackoff waits ~0.5s, 1s, 2s, 4s, 8s with small jitter.
// Returns false when ctx is cancelled during the wait.
func sleepStreamRetryBackoff(ctx context.Context, attempt int) bool {
	// attempt is 1-based for the failed attempt about to be retried.
	shift := min(max(attempt-1, 0), 4)
	base := time.Duration(1<<shift) * 500 * time.Millisecond
	jitter := time.Duration(rand.Intn(250)) * time.Millisecond
	timer := time.NewTimer(base + jitter)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// handleFinalResponse processes a no-tool assistant turn: recovery pause,
// readiness boundary, empty-final retry, executor handoff nudge, steer drain,
// and final compaction. cont=true continues the tool loop; cont=false returns
// err from Run (err may be nil for a clean final answer).
func (a *Agent) handleFinalResponse(ctx context.Context, state *turnRuntime, text, reasoning string, usage *provider.Usage) (cont bool, err error) {
	// Recovery finalization produced a summary. Keep it in the session,
	// but still pause so Goal auto-continue cannot open another Run with
	// a fresh finalization round. turn_done reports recovery_paused.
	if state.recoveryGraceRound {
		a.contextManager().ObserveUsage(usage)
		reason := ""
		if ctrl := a.recoveryEpisodeControl(); ctrl != nil {
			_, _ = ctrl.ConsumeFinalization(a.recovery.taskID)
		}
		return false, &RecoveryPauseError{
			Message:    "Automatic retries paused. Reasonix stopped repeated attempts and kept completed work. Send \"continue\" to start a fresh attempt, or add instructions to change direction.",
			StopReason: reason,
		}
	}
	readiness := a.finalReadinessCheckFor()
	if state.graceRound && (readiness.reason != "" || !hasVisibleFinalAnswer(text)) {
		a.contextManager().ObserveUsage(usage)
		return false, a.gracePause(state)
	}
	if state.graceRound {
		// Explicit max_steps and spend budgets are user-selected boundaries.
		// Preserve the summary, then return a resumable pause so Goal does not
		// immediately open another Run and silently bypass the chosen limit.
		a.contextManager().ObserveUsage(usage)
		return false, a.gracePause(state)
	}
	if readiness.reason != "" {
		// Standard ends with its answer/quality summary. Delivery and Goal hand
		// the structured gap to the controller, which exposes an explicit recovery
		// action or lets the Goal FSM decide whether to continue.
		if a.readinessPauseActive(readiness) {
			event.RecordReadinessAudit(a.svc.sink, readiness.audit(evidence.ReadinessErrored, false))
			a.pending.finalReadinessRecovery = true
			a.persistFinalReadinessRecovery(readiness.missingIDs())
			return false, &FinalReadinessError{
				Attempts:          1,
				Reason:            readiness.reason,
				Missing:           readiness.missingIDs(),
				ContinuationClass: readiness.continuationClass(),
				ProgressKey:       readiness.progressSignature(),
			}
		}
		event.RecordReadinessAudit(a.svc.sink, readiness.audit(evidence.ReadinessAllowed, a.turn.readinessRecovered))
	}
	if cont, err, handled := a.finishRequiredAtResponseEnd(ctx, state, text, usage); handled {
		return cont, err
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
		if a.requireVisibleFinal || !reasoningOnlyFinishHonoured(a.svc.prov, usage, reasoning) {
			state.emptyFinalBlocks++
			if state.emptyFinalBlocks >= maxEmptyFinalBlocks {
				return false, fmt.Errorf("model finished without a visible final answer %d times", state.emptyFinalBlocks)
			}
			a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeEmptyFinal, Text: emptyFinalNotice(), Detail: emptyFinalNoticeDetail(a.svc.prov.Name(), usage, len(reasoning))})
			a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(emptyFinalRetryMessage())})
			a.contextManager().ObserveUsage(usage)
			return true, nil
		}
	}
	if state.executorHandoff && !state.usedAnyTool && state.handoffNudges < maxExecutorHandoffNudges && shouldNudgeExecutorHandoff(state.input, text) {
		state.handoffNudges++
		a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeExecutorHandoff, Text: executorHandoffNoticeText(), Detail: "executor answered without taking any action; nudging it to use its tools"})
		a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(executorHandoffRetryMessage())})
		a.contextManager().ObserveUsage(usage)
		return true, nil
	}
	if a.continueStandardTodo(ctx, state) {
		a.contextManager().ObserveUsage(usage)
		return true, nil
	}
	if readiness.applies || a.turn.readinessRecovered {
		event.RecordReadinessAudit(a.svc.sink, readiness.audit(evidence.ReadinessAllowed, a.turn.readinessRecovered))
	}
	a.emitTurnShadows(a.turn.turnInput)
	if !a.closeSteerIntakeIfIdle() {
		return true, nil
	}
	// A final-answer turn otherwise skips compaction, so a large context
	// carries into the next turn un-folded and can overflow the model window.
	// No-op below the trigger, so normal turns keep their warm cache.
	a.contextManager().ObserveUsage(usage)
	return false, nil // model gave a final answer
}

// handleToolRound executes a tool batch, persists tool messages, handles
// cancellation, todo stall tracking, recovery finalization pause, and the
// max-steps grace round. cont=true continues the tool loop; cont=false returns
// err from Run.
func (a *Agent) handleToolRound(ctx context.Context, state *turnRuntime, step int, text, reasoning string, calls []provider.ToolCall, usage *provider.Usage) (cont bool, err error) {
	state.emptyFinalBlocks = 0
	state.usedAnyTool = true
	if cont, err, handled := a.rejectMixedFinishBatch(state, text, calls, usage); handled {
		return cont, err
	}
	unavailableContextTools := a.unavailableContextualToolCalls(ctx, calls)
	if len(unavailableContextTools) > 0 && state.contextToolRepairs > 0 {
		msg := fmt.Sprintf("blocked: context-unavailable tools were called again after the repair instruction: %s", strings.Join(unavailableContextTools, ", "))
		for _, call := range calls {
			a.sess.conversation.Add(provider.Message{Role: provider.RoleTool, Content: msg, ToolCallID: call.ID, Name: call.Name})
		}
		if hasVisibleFinalAnswer(text) {
			return a.handleFinalResponse(ctx, state, text, reasoning, usage)
		}
		if len(unavailableContextTools) == 1 && unavailableContextTools[0] == "update_goal" {
			return false, fmt.Errorf("model repeatedly called update_goal outside Goal mode without a visible answer")
		}
		return false, fmt.Errorf("model repeatedly called context-unavailable tools without a visible answer: %s", strings.Join(unavailableContextTools, ", "))
	}

	boundaryFinalizer := a.allowsBoundaryTurnFinalizer(ctx, state, calls)
	if boundaryErr, stop := a.stopUnexecutedBoundaryCalls(ctx, state, calls, usage); stop {
		return false, boundaryErr
	}

	receiptMark := 0
	if a.task.ledger != nil {
		receiptMark = a.task.ledger.Len()
	}
	batch := a.executeBatch(ctx, state, calls)
	if batch.err != nil {
		// No call from the batch has executed: executeBatch commits every full
		// dispatch before entering its execution scheduler.
		return false, batch.err
	}
	results, images := batch.results, batch.images
	for i, call := range calls {
		msg := provider.Message{
			Role:       provider.RoleTool,
			Content:    results[i],
			Images:     images[i],
			ToolCallID: call.ID,
			Name:       call.Name,
		}
		// Content is the stable bounded provider form. Full originals remain in
		// local RawContent and enter model context only through explicit paging.
		if i < len(batch.outcomes) && batch.outcomes[i].rawOutput != "" && batch.outcomes[i].rawOutput != results[i] {
			msg.RawContent = batch.outcomes[i].rawOutput
		}
		if i < len(batch.executions) {
			msg.ToolExecution = toProviderToolExecution(batch.executions[i])
		}
		a.sess.conversation.Add(msg)
	}
	// If the context was cancelled during tool execution, return after storing
	// the batch results so the session keeps paired tool-call history.
	if ctx.Err() != nil {
		a.recordInterruptedDisplay("", "", nil, true, state.workDurationMs())
		return false, ctx.Err()
	}
	if a.successfulTurnFinalizer(ctx, calls, batch) {
		if a.finalizerName(calls) == "finish" {
			if cont, err := a.acceptFinishCall(state, text, calls); cont || err != nil {
				return cont, err
			}
		}
		// submit_plan is the planner's data-bearing final answer. Its paired tool
		// result is stored, so another acknowledgement adds no host value and can
		// turn a valid bounded plan into a max-steps pause.
		a.contextManager().ObserveUsage(usage)
		return false, nil
	}
	if cont, err, handled := a.repairRejectedFinish(state, text, calls, usage); handled {
		return cont, err
	}
	if boundaryFinalizer {
		// The one allowed boundary finalizer ran but was rejected or blocked.
		// Preserve the one-grace-round contract instead of opening an unbounded
		// loop of malformed terminal submissions.
		a.contextManager().ObserveUsage(usage)
		return false, a.gracePause(state)
	}
	if len(unavailableContextTools) > 0 {
		if hasVisibleFinalAnswer(text) {
			// Keep the assistant tool call and host error paired in the transcript,
			// but accept a co-streamed answer without another repair request.
			return a.handleFinalResponse(ctx, state, text, reasoning, usage)
		}
		state.contextToolRepairs++
		nudge := fmt.Sprintf("The following tools are unavailable in the current workflow phase: %s. Do not call them again. Respond to the user's request with visible answer text now; call a different tool only if it is still needed to complete the request.", strings.Join(unavailableContextTools, ", "))
		a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(nudge)})
	}
	a.trackTodoProgress(ctx, state, receiptMark)

	// The prompt only grows from here; compact before the next turn so it
	// stays within the model's window.
	a.contextManager().ObserveUsage(usage)

	// When Auto recovery exhausts its Episode budget, offer exactly one
	// summarize-only finalization round. Successful summary ends cleanly;
	// further tool calls surface RecoveryPauseError.
	if batch.recoveryStopTurn && !state.recoveryGraceRound {
		state.recoveryGraceRound = true
		if ctrl := a.recoveryEpisodeControl(); ctrl != nil {
			ctrl.MarkFinalizationOffered(a.recovery.taskID)
		}
		nudge := "Auto recovery has reached its limit for this turn. Do not call any more tools. Summarize what was completed, what failed, and what the user should do next. The user can continue in the next message."
		a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(nudge)})
		return true, nil
	}

	// Spend is checked before rounds: it is the axis a runaway is actually
	// reported in, so on the turns both would catch it should be the one named.
	if axis, detail := a.task.budget.exceeded(a.taskBudgetLimit(ctx)); axis != "" {
		a.armFinalizationRound(ctx, state, landCause{kind: "task_budget", axis: axis, detail: detail})
		return true, nil
	}
	if state.runMaxSteps > 0 && step+1 >= state.runMaxSteps {
		a.armFinalizationRound(ctx, state, landCause{kind: "max_steps", detail: fmt.Sprintf(
			"budget (%s=%d) exhausted: one grace round to finalize", state.runMaxStepsKey, state.runMaxSteps)})
	}
	return true, nil
}

func (a *Agent) pairUnexecutedGraceCalls(calls []provider.ToolCall, msg string) {
	for _, call := range calls {
		a.sess.conversation.Add(provider.Message{Role: provider.RoleTool, Content: msg, ToolCallID: call.ID, Name: call.Name})
	}
}

func (a *Agent) unavailableContextualToolCalls(ctx context.Context, calls []provider.ToolCall) []string {
	if len(calls) == 0 {
		return nil
	}
	if a == nil || a.svc.tools == nil {
		return nil
	}
	names := make([]string, 0, len(calls))
	seen := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		t, canonical, ambiguous := a.svc.tools.ResolveCall(call.Name)
		if t == nil || len(ambiguous) > 0 {
			continue
		}
		contextual, ok := t.(tool.ContextualTool)
		if !ok || contextual.ProviderVisible(ctx) {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		names = append(names, canonical)
	}
	return names
}
