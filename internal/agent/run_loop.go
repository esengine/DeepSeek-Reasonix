package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"reasonix/internal/agentpreset"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/taskintent"
	"reasonix/internal/taskpolicy"
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
	calls              []provider.ToolCall
	responsesItems     []json.RawMessage
	usage              *provider.Usage
	interrupted        bool
	partialToolStarted bool
	partialCalls       []provider.ToolCall
	maxArgChars        int // peak streaming tool-arg size for failed-attempt estimates
	err                error
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
	if s.waitingForReasoning && !s.sawReasoning && e.Kind == event.ToolDispatch {
		s.events = append(s.events, e)
		return
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
func (a *Agent) beginRunTurn(ctx context.Context, input string) (rawInput string, state *runLoopState) {
	rawInput = RawUserInput(ctx, input)
	providerInput := input
	// A fresh user turn starts from zeroed per-turn host state; the new turn's
	// values are computed below. Cross-turn state (checkpoint, scope, failure
	// budgets) lives directly on Agent and is reconciled field by field.
	a.perTurnState = perTurnState{}
	a.resetStructuralRunGuards()
	scope, scoped := DeliveryExecutionScopeFromContext(ctx)
	preserveEvidence := a.preserveEvidenceOnce
	// A run that starts with a pending readiness recovery (or an explicit
	// evidence-preserving continuation) and then passes readiness counts as a
	// recovery in the final audit.
	a.readinessRecovered = preserveEvidence || a.deliveryRecoveryPending
	if a.evidence != nil {
		switch {
		case preserveEvidence:
			a.evidence.ResetBackgroundLeases()
		case scoped && a.deliveryScopeID == scope.ID:
			a.evidence.ResetBackgroundLeases()
		default:
			a.resetTurnEvidence()
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
	a.turnInput = a.classifierTaskText
	if scoped && strings.TrimSpace(scope.TaskText) != "" {
		a.turnInput = scope.TaskText
	} else if strings.TrimSpace(a.turnInput) == "" {
		a.turnInput = rawInput
	}
	intent := taskintent.Classify(a.turnInput)
	a.deliveryTaskExpected = intent.NeedsEvidence()
	a.deliveryMutationExpected = intent == taskintent.Mutation && registryHasWriterTools(a.tools)
	a.deliveryPersistentExpected = taskintent.NeedsPersistentAction(a.turnInput)
	a.recoveryTaskSummary = boundedRecoveryTaskSummary(a.turnInput)
	// Freeze TaskPolicy for this turn from the session role setting. Subsequent
	// SetAgentPreset calls must not change this turn's route/review floor.
	if policy, ok := taskpolicy.FromContext(ctx); ok {
		a.turnPolicy = policy
	} else {
		a.turnPolicy = taskpolicy.Derive(taskpolicy.Input{
			Raw:         a.turnInput,
			Instruction: taskpolicy.StripQuotedConstraints(a.turnInput),
			Preset:      agentpreset.AgentPreset(a.AgentPreset()),
			PlanMode:    a.planMode.Load(),
		})
	}
	a.turnPolicySet = true
	// Align legacy delivery gates with the frozen role setting. Delivery always
	// enables the full readiness contract. Light/Balanced only elevate when the
	// turn is a mutation that requires forced review or is high-risk.
	switch {
	case a.AgentPreset() == string(agentpreset.Delivery):
		a.deliveryProfile = true
	case a.turnPolicy.Intent == taskintent.Mutation &&
		(a.turnPolicy.RequiresIndependentReview() || a.turnPolicy.Risk >= taskpolicy.RiskHigh):
		a.deliveryProfile = true
	default:
		a.deliveryProfile = false
	}
	// A cancelled/error turn leaves a provider-excluded recovery record at the
	// transcript tail. Fold its bounded facts into this new user turn exactly
	// once; the user's raw text remains the classifier source above.
	providerInput = withInterruptedRecovery(providerInput, a.pendingInterruptedRecovery())
	a.prepareRepeatFailureScope(scoped, scope.ID)
	a.sink.Emit(event.Event{Kind: event.TurnStarted})
	a.emitTurnPhase(event.TurnPhaseWorking)
	input = a.withTurnPreferences(providerInput)
	// Persist the short execution-policy block in provider Content; keep the
	// original user text in RawContent for history/title/rewind stripping.
	policyBlock := taskpolicy.ExecutionPolicyBlock(a.turnPolicy)
	if !strings.Contains(input, "<execution-policy") {
		input = strings.TrimSpace(input) + "\n\n" + policyBlock
	}
	userCreatedAt := time.Now().UnixMilli()
	a.activeTurnCreatedAt.Store(userCreatedAt)
	rawContent := rawInput
	if rawContent == "" {
		rawContent = a.turnInput
	}
	a.session.Add(provider.Message{
		Role: provider.RoleUser, Content: input, RawContent: rawContent,
		Images: userImages(ctx), CreatedAt: userCreatedAt,
	})

	state = &runLoopState{
		emptyFinalBlocks:   0,
		handoffNudges:      0,
		usedAnyTool:        false,
		graceRound:         false,
		recoveryGraceRound: false,
		todoStallRounds:    0,
		seenTodoProgress:   make(map[string]struct{}),
		executorHandoff:    a.executorHandoffGuard && strings.Contains(input, executorHandoffMarker),
		input:              input,
		budget:             runBudget{started: time.Now()},
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
	ctx = a.withAgentContext(ctx)
	for step := 0; state.runMaxSteps <= 0 || step < state.runMaxSteps || state.graceRound || state.recoveryGraceRound; step++ {
		// Consume a queued steer and persist it to the session so it
		// survives tab switches and history replay. The model sees it as
		// guidance (with a prefix), not a new task. One cache miss per
		// steer is unavoidable — the model must see the new instruction.
		if text, itemID, ok := a.consumeSteer(); ok {
			a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(midTurnSteerMessage(text))})
			a.sink.Emit(event.Event{Kind: event.Steer, Text: text, ItemID: itemID})
		} else if itemID != "" {
			// Loader failed after dequeue: durable entry stays for inspection
			// (unapplied path marks uncertain + pause via the notice sink).
			a.RecordUnappliedSteer("(body load failed)", itemID)
		}
		schemas := a.tools.Schemas()
		prefixShape := a.capturePrefixShape(schemas)
		prevPrefixShape := a.lastPrefixShape
		if !a.haveLastPrefixShape {
			prevPrefixShape = prefixShape
		}

		// Drain reasons queued since the previous capture (compaction,
		// snip/prune, rewind, guardian merge) so CompareShape can attribute
		// any prefix change to the operation that actually caused it, instead
		// of a generic rewrite signal that also fires on local-only metadata
		// edits.
		contentReasons := a.session.DrainContentRewriteReasons()

		// Prefix shape is captured once before sampling and frozen for the
		// whole attempt lifecycle — stream retries must not rewrite session
		// history mid-round, so the shape stays stable across body replays.
		streamed := a.streamWithSamplingRecovery(ctx, step+1)
		text, reasoning, signature, calls, responsesItems, usage := streamed.text, streamed.reasoning, streamed.signature, streamed.calls, streamed.responsesItems, streamed.usage
		partialCalls, err := streamed.partialCalls, streamed.err
		cacheDiagnostics := CompareShape(prevPrefixShape, prefixShape, usage, contentReasons)
		if err != nil {
			a.emitTurnUsage(usage, &cacheDiagnostics)
			a.observeRunBudget(state, usage)
			if msg, ok := finishReasonMessage(usage); ok {
				a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: msg})
			}
			// Exhausted stream retries (or a non-retryable error): persist one
			// bounded LocalOnly recovery record for the next real user message.
			// Intermediate failed attempts never wrote session state.
			a.recordInterruptedDisplay(text, reasoning, partialCalls, true, state.workDurationMs())
			return err
		}
		a.lastPrefixShape = prefixShape
		a.haveLastPrefixShape = true
		a.emitTurnUsage(usage, &cacheDiagnostics)
		a.observeRunBudget(state, usage)
		if msg, ok := finishReasonMessage(usage); ok {
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: msg})
		}

		// Commit boundary: only a clean terminal attempt reaches here.
		// Keep reasoning_content on the assistant turn for display and session
		// archive. Most OpenAI-compatible backends do not replay it; providers
		// with an explicit round-trip contract retain the raw provider text.
		calls = a.withPreviewFileDiffs(ctx, calls)
		a.session.Add(provider.Message{
			Role:               provider.RoleAssistant,
			Content:            text,
			ReasoningContent:   reasoning,
			ReasoningSignature: signature,
			ReasoningID:        streamed.reasoningID,
			ReasoningStatus:    streamed.reasoningStatus,
			ToolCalls:          calls,
			ResponsesItems:     responsesItems,
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

	var billable *provider.Usage
	var last streamedTurn

	runAttempt := func(attemptID string, sink event.Sink) streamedTurn {
		before := provider.RequestAttemptCount(ctx)
		result := a.streamWithFrozen(ctx, turn, sink, &frozen, attemptID)
		after := provider.RequestAttemptCount(ctx)
		delta := max(after-before, 0)
		// httpRequests=0 means the provider does not use SendWithRetry
		// (extension/custom), or it failed before issuing an HTTP request.
		// Only overwrite RequestCount when the built-in counter observed POSTs;
		// otherwise keep the provider-reported count (zero still means one via
		// usageRequestCount compatibility). estimateFailedAttemptUsage returns nil
		// for zero-output local failures so no invented request appears.
		result.usage = estimateFailedAttemptUsage(result.usage, frozen, result, delta)
		if result.usage != nil {
			if delta > 0 {
				result.usage.RequestCount = delta
			}
		} else if delta > 0 {
			result.usage = &provider.Usage{RequestCount: delta}
		}
		return result
	}

	for attempt := 1; attempt <= maxSamplingAttempts; attempt++ {
		attemptID := newStreamAttemptID(attempt)
		a.emitStreamAttempt(attemptID, event.StreamAttemptBegin, attempt, "", nil)

		var streamSink *deferredStreamSink
		attemptSink := a.sink
		if provider.WarnOnMissingToolCallReasoning(a.prov) {
			streamSink = newReasoningAwareStreamSink(a.sink)
			attemptSink = streamSink
		}

		result := runAttempt(attemptID, attemptSink)
		billable = mergeSamplingUsage(billable, result.usage)
		// lastUsage is the latest single-request shape (prompt+completion+cache
		// for that attempt only). Never the multi-attempt billable aggregate —
		// that would inflate ContextSnapshot and compaction decisions.
		a.storeLatestRequestUsage(result.usage)
		last = result
		last.usage = finalizeSamplingUsage(billable, result.usage)

		if result.err != nil {
			retry, terminal := a.handleSamplingError(ctx, attemptID, attempt, streamSink, &frozen, result, last, billable)
			if retry {
				continue
			}
			return terminal
		}

		// Clean terminal. Optionally repair missing reasoning with one extra
		// exact replay of the same frozen request (no synthetic prompt).
		missing, shouldRetry := a.observeMissingToolCallReasoning(result.calls, result.reasoning)
		if missing {
			event.RecordProtocolRecovery(a.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningDetected})
			if shouldRetry && strings.TrimSpace(result.text) == "" {
				event.RecordProtocolRecovery(a.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetryAttempted})
				retrySink := newDeferredStreamSink(a.sink)
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
					// Fall back to the first complete response; no tool ran.
					streamSink.Flush()
					a.storeLatestRequestUsage(result.usage)
					result.usage = finalizeSamplingUsage(billable, result.usage)
					event.RecordProtocolRecovery(a.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningFallback})
					a.emitStreamAttempt(attemptID, event.StreamAttemptCommit, attempt, "", nil)
					return result
				}
				streamSink.Discard()
				retrySink.Flush()
				a.storeLatestRequestUsage(retry.usage)
				retry.usage = finalizeSamplingUsage(billable, retry.usage)
				retryMissing, _ := a.observeMissingToolCallReasoning(retry.calls, retry.reasoning)
				if retryMissing {
					event.RecordProtocolRecovery(a.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningDetected})
					event.RecordProtocolRecovery(a.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningFallback})
				} else if len(retry.calls) == 0 {
					event.RecordProtocolRecovery(a.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetryReplaced})
				} else {
					event.RecordProtocolRecovery(a.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetryRecovered})
				}
				a.emitStreamAttempt(attemptID, event.StreamAttemptCommit, attempt, "", nil)
				return retry
			}
			if !shouldRetry || strings.TrimSpace(result.text) != "" {
				event.RecordProtocolRecovery(a.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetrySuppressed})
				event.RecordProtocolRecovery(a.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningFallback})
			} else {
				event.RecordProtocolRecovery(a.sink, event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningFallback})
			}
		}

		streamSink.Flush()
		a.emitStreamAttempt(attemptID, event.StreamAttemptCommit, attempt, "", nil)
		result.usage = finalizeSamplingUsage(billable, result.usage)
		return result
	}
	return last
}

func (a *Agent) emitStreamAttempt(id string, action event.StreamAttemptAction, attempt int, reason string, err error) {
	if reason == "" && err != nil {
		reason = provider.StreamInterruptReason(err)
	}
	a.sink.Emit(event.Event{
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
// readiness retry, empty final retry, executor handoff nudge, steer drain, and
// final compaction. cont=true continues the tool loop; cont=false returns err
// from Run (err may be nil for a clean final answer).
func (a *Agent) handleFinalResponse(ctx context.Context, state *runLoopState, text, reasoning string, usage *provider.Usage) (cont bool, err error) {
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
	if state.graceRound && (state.landCause.kind == "task_budget" || !state.runLimitHostOwned) {
		// Explicit max_steps and spend budgets are user-selected boundaries.
		// Preserve the summary, then return a resumable pause so Goal does not
		// immediately open another Run and silently bypass the chosen limit.
		a.contextManager().ObserveUsage(usage)
		return false, a.gracePause(state)
	}
	if readiness.reason != "" {
		// Delivery no longer retries readiness with hidden model messages: the
		// run ends immediately with the missing requirements, and the host owns
		// what happens next. In Goal mode the FSM auto-continues under budget
		// with the missing list as the next turn; plain Delivery turns surface
		// the recovery card for an explicit user continuation.
		event.RecordReadinessAudit(a.sink, readiness.audit(evidence.ReadinessErrored, false))
		a.deliveryRecoveryPending = true
		return false, &FinalReadinessError{Attempts: 1, Reason: readiness.reason, Missing: readiness.missingIDs()}
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
		if a.requireVisibleFinal || !reasoningOnlyFinishHonoured(a.prov, usage, reasoning) {
			state.emptyFinalBlocks++
			if state.emptyFinalBlocks >= maxEmptyFinalBlocks {
				return false, fmt.Errorf("model finished without a visible final answer %d times", state.emptyFinalBlocks)
			}
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeEmptyFinal, Text: emptyFinalNotice(), Detail: emptyFinalNoticeDetail(a.prov.Name(), usage, len(reasoning))})
			a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(emptyFinalRetryMessage())})
			a.contextManager().ObserveUsage(usage)
			return true, nil
		}
	}
	if state.executorHandoff && !state.usedAnyTool && state.handoffNudges < maxExecutorHandoffNudges && shouldNudgeExecutorHandoff(state.input, text) {
		state.handoffNudges++
		a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeExecutorHandoff, Text: executorHandoffNoticeText(), Detail: "executor answered without taking any action; nudging it to use its tools"})
		a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(executorHandoffRetryMessage())})
		a.contextManager().ObserveUsage(usage)
		return true, nil
	}
	if readiness.applies {
		event.RecordReadinessAudit(a.sink, readiness.audit(evidence.ReadinessAllowed, a.readinessRecovered))
	}
	a.emitTurnShadows(a.turnInput)
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
func (a *Agent) handleToolRound(ctx context.Context, state *runLoopState, step int, text, reasoning string, calls []provider.ToolCall, usage *provider.Usage) (cont bool, err error) {
	state.emptyFinalBlocks = 0
	state.usedAnyTool = true
	unavailableContextTools := a.unavailableContextualToolCalls(ctx, calls)
	if len(unavailableContextTools) > 0 && state.contextToolRepairs > 0 {
		msg := fmt.Sprintf("blocked: context-unavailable tools were called again after the repair instruction: %s", strings.Join(unavailableContextTools, ", "))
		for _, call := range calls {
			a.session.Add(provider.Message{Role: provider.RoleTool, Content: msg, ToolCallID: call.ID, Name: call.Name})
		}
		if hasVisibleFinalAnswer(text) {
			return a.handleFinalResponse(ctx, state, text, reasoning, usage)
		}
		if len(unavailableContextTools) == 1 && unavailableContextTools[0] == "update_goal" {
			return false, fmt.Errorf("model repeatedly called update_goal outside Goal mode without a visible answer")
		}
		return false, fmt.Errorf("model repeatedly called context-unavailable tools without a visible answer: %s", strings.Join(unavailableContextTools, ", "))
	}

	if boundaryErr, stop := a.stopUnexecutedBoundaryCalls(state, calls, usage); stop {
		return false, boundaryErr
	}

	receiptMark := 0
	if a.evidence != nil {
		receiptMark = a.evidence.Len()
	}
	batch := a.executeBatch(ctx, calls)
	results, images := batch.results, batch.images
	for i, call := range calls {
		msg := provider.Message{
			Role:       provider.RoleTool,
			Content:    results[i],
			Images:     images[i],
			ToolCallID: call.ID,
			Name:       call.Name,
		}
		// First-visible Content is always the bounded form in results[i].
		// Full originals ride on RawContent only when truncation applied.
		if i < len(batch.outcomes) && batch.outcomes[i].rawOutput != "" && batch.outcomes[i].rawOutput != results[i] {
			msg.RawContent = batch.outcomes[i].rawOutput
		}
		if i < len(batch.executions) {
			msg.ToolExecution = toProviderToolExecution(batch.executions[i])
		}
		a.session.Add(msg)
	}
	// If the context was cancelled during tool execution, return after storing
	// the batch results so the session keeps paired tool-call history.
	if ctx.Err() != nil {
		a.recordInterruptedDisplay("", "", nil, true, state.workDurationMs())
		return false, ctx.Err()
	}
	if len(unavailableContextTools) > 0 {
		if hasVisibleFinalAnswer(text) {
			// Keep the assistant tool call and host error paired in the transcript,
			// but accept a co-streamed answer without another repair request.
			return a.handleFinalResponse(ctx, state, text, reasoning, usage)
		}
		state.contextToolRepairs++
		nudge := fmt.Sprintf("The following tools are unavailable in the current workflow phase: %s. Do not call them again. Respond to the user's request with visible answer text now; call a different tool only if it is still needed to complete the request.", strings.Join(unavailableContextTools, ", "))
		a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(nudge)})
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
		a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(nudge)})
		return true, nil
	}

	// Spend is checked before rounds: it is the axis a runaway is actually
	// reported in, so on the turns both would catch it should be the one named.
	if axis, detail := a.taskBudget.exceeded(a.taskBudgetLimit(ctx)); axis != "" {
		a.armFinalizationRound(state, landCause{kind: "task_budget", axis: axis, detail: detail})
		return true, nil
	}
	if state.runMaxSteps > 0 && step+1 >= state.runMaxSteps {
		a.armFinalizationRound(state, landCause{kind: "max_steps", detail: fmt.Sprintf(
			"budget (%s=%d) exhausted: one grace round to finalize", state.runMaxStepsKey, state.runMaxSteps)})
	}
	return true, nil
}

func (a *Agent) pairUnexecutedGraceCalls(calls []provider.ToolCall, msg string) {
	for _, call := range calls {
		a.session.Add(provider.Message{Role: provider.RoleTool, Content: msg, ToolCallID: call.ID, Name: call.Name})
	}
}

func (a *Agent) unavailableContextualToolCalls(ctx context.Context, calls []provider.ToolCall) []string {
	if len(calls) == 0 {
		return nil
	}
	if a == nil || a.tools == nil {
		return nil
	}
	names := make([]string, 0, len(calls))
	seen := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		t, canonical, ambiguous := a.tools.ResolveCall(call.Name)
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
