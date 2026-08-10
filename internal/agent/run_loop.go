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
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/taskintent"
	"reasonix/internal/tool"
)

// runLoopState holds per-Run loop counters and flags. It is package-private and
// not shared across goroutines; the first extraction keeps the existing lock
// model and only structures the sequential turn state machine.
type runLoopState struct {
	runMaxSteps       int
	runMaxStepsKey    string
	runLimitHostOwned bool

	emptyFinalBlocks   int
	handoffNudges      int
	usedAnyTool        bool
	contextToolRepairs int
	graceRound         bool
	recoveryGraceRound bool

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
	a.perTurnState = perTurnState{turnInput: input}
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
	classifierInput := a.classifierTaskText
	if scoped && strings.TrimSpace(scope.TaskText) != "" {
		classifierInput = scope.TaskText
	} else if strings.TrimSpace(classifierInput) == "" {
		classifierInput = rawInput
	}
	intent := taskintent.Classify(classifierInput)
	a.deliveryTaskExpected = intent.NeedsEvidence()
	a.deliveryMutationExpected = intent == taskintent.Mutation && registryHasWriterTools(a.tools)
	a.deliveryPersistentExpected = taskintent.NeedsPersistentAction(classifierInput)
	a.recoveryTaskSummary = boundedRecoveryTaskSummary(classifierInput)
	// A cancelled/error turn leaves a provider-excluded recovery record at the
	// transcript tail. Fold its bounded facts into this new user turn exactly
	// once; the user's raw text remains the classifier source above.
	providerInput = withInterruptedRecovery(providerInput, a.pendingInterruptedRecovery())
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
		emptyFinalBlocks:   0,
		handoffNudges:      0,
		usedAnyTool:        false,
		graceRound:         false,
		recoveryGraceRound: false,
		todoStallRounds:    0,
		seenTodoProgress:   make(map[string]struct{}),
		executorHandoff:    a.executorHandoffGuard && strings.Contains(input, executorHandoffMarker),
		input:              input,
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
	return &maxStepsPause{steps: state.runMaxSteps, key: state.runMaxStepsKey}
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

// estimateFailedAttemptUsage fills Estimated usage when a body attempt ends
// without a terminal provider usage record, so billing and observational Goal
// usage still include the issued request plus any observed speculative output.
// Non-interrupt failures that already carry usage (e.g. client reasoning limit)
// are left intact.
//
// httpRequests is the SendWithRetry attempt-counter delta for this body attempt.
// When it is 0 and there was no speculative output, the failure was local or
// came from a provider without observable transport accounting; return nil or
// its existing usage rather than inventing billable tokens.
func estimateFailedAttemptUsage(usage *provider.Usage, frozen samplingRequest, result streamedTurn, httpRequests int) *provider.Usage {
	if result.err == nil {
		return usage
	}
	// Preserve exact client-side finish reasons that already computed usage.
	if usage != nil && usage.FinishReason != "" && usage.FinishReason != "interrupted" {
		return usage
	}
	// A zero-output, non-interrupted failure with no observed HTTP request is a
	// local/provider validation failure. It is not a billable sampling attempt.
	preBodyLocal := httpRequests <= 0 && !result.interrupted &&
		!provider.IsStreamInterrupted(result.err) && !sawSpeculativeSamplingOutput(result)
	if preBodyLocal {
		if usage != nil && usageTotalTokens(usage) > 0 {
			return usage
		}
		return nil
	}
	if !provider.IsStreamInterrupted(result.err) && !result.interrupted {
		// Auth/cancel/decode/limit paths keep their own accounting.
		if usage != nil {
			return usage
		}
		if httpRequests <= 0 {
			return nil
		}
	}
	textBytes := len(result.text)
	reasoningBytes := len(result.reasoning)
	maxArg := result.maxArgChars
	for _, call := range result.partialCalls {
		if n := len(call.Arguments); n > maxArg {
			maxArg = n
		}
	}
	for _, call := range result.calls {
		if n := len(call.Arguments); n > maxArg {
			maxArg = n
		}
	}
	if usage != nil && !usage.Estimated && usage.TotalTokens > 0 {
		return usage
	}
	finish := "interrupted"
	if usage != nil && usage.FinishReason != "" {
		finish = usage.FinishReason
	}
	est := bestEffortStreamUsage(usage, textBytes, reasoningBytes, finish)
	if est == nil {
		est = &provider.Usage{Estimated: true, FinishReason: finish}
	}
	if est.PromptTokens <= 0 {
		est.PromptTokens = estimateSamplingRequestInputTokens(frozen.req)
		est.Estimated = true
	}
	// Estimated failed attempts without cache split still need Cost() to see
	// billable input — Price falls back to PromptTokens only when hit+miss=0.
	if est.CacheHitTokens+est.CacheMissTokens == 0 && est.PromptTokens > 0 {
		est.CacheMissTokens = est.PromptTokens
	}
	if maxArg > 0 {
		argTokens := (maxArg + 3) / 4
		if est.CompletionTokens < argTokens+estimateTokensFromBytes(textBytes)+estimateTokensFromBytes(reasoningBytes) {
			est.CompletionTokens = argTokens + estimateTokensFromBytes(textBytes) + estimateTokensFromBytes(reasoningBytes)
			est.Estimated = true
		}
	}
	if minTotal := est.PromptTokens + est.CompletionTokens; est.TotalTokens < minTotal {
		est.TotalTokens = minTotal
		est.Estimated = true
	}
	return est
}

func sawSpeculativeSamplingOutput(result streamedTurn) bool {
	return result.text != "" || result.reasoning != "" || result.maxArgChars > 0 ||
		result.partialToolStarted || len(result.calls) > 0 || len(result.partialCalls) > 0
}

// estimateSamplingRequestInputTokens reconstructs a conservative input count
// only when an interrupted attempt closed before terminal provider usage. It is
// accounting telemetry, not request admission: the estimate never changes the
// frozen provider request or imposes a token ceiling.
func estimateSamplingRequestInputTokens(req provider.Request) int {
	total := 3
	for _, msg := range provider.ModelMessages(req.Messages) {
		total += 4
		total += estimateTextTokens(msg.Content)
		total += estimateTextTokens(msg.ReasoningContent)
		total += estimateTextTokens(msg.ReasoningSignature)
		total += estimateTextTokens(msg.Name)
		total += estimateTextTokens(msg.ToolCallID)
		for _, image := range msg.Images {
			total += estimateTextTokens(image)
		}
		for _, call := range msg.ToolCalls {
			total += 8 + estimateTextTokens(call.ID) + estimateTextTokens(call.Name) + estimateTextTokens(call.Arguments)
		}
		for _, item := range msg.ResponsesItems {
			total += estimateTextTokens(string(item))
		}
	}
	for _, schema := range req.Tools {
		encoded, _ := json.Marshal(schema)
		total += 8 + estimateTextTokens(string(encoded))
	}
	return max(total, 1)
}

// mergeSamplingUsage accumulates billable counters across body attempts.
// PromptTokens is the billable input total (aligned with cache hit+miss).
// ContextPromptTokens is set later by finalizeSamplingUsage from the latest attempt.
func mergeSamplingUsage(acc, attempt *provider.Usage) *provider.Usage {
	if attempt == nil {
		return acc
	}
	billableHitMiss := func(u *provider.Usage) (hit, miss int) {
		if u == nil {
			return 0, 0
		}
		if u.CacheHitTokens+u.CacheMissTokens > 0 {
			return u.CacheHitTokens, u.CacheMissTokens
		}
		// No cache split: treat PromptTokens as uncached billable input.
		return 0, u.PromptTokens
	}
	billablePrompt := func(hit, miss, prompt int) int {
		if hit+miss > 0 {
			return hit + miss
		}
		return prompt
	}
	if acc == nil {
		merged := *attempt
		if merged.RequestCount <= 0 {
			merged.RequestCount = 1
		}
		hit, miss := billableHitMiss(attempt)
		merged.CacheHitTokens = hit
		merged.CacheMissTokens = miss
		merged.PromptTokens = billablePrompt(hit, miss, attempt.PromptTokens)
		return &merged
	}
	merged := *acc
	// Billable input for Cost: sum hit/miss (prompt when no cache split).
	ah, am := billableHitMiss(acc)
	bh, bm := billableHitMiss(attempt)
	// If acc was previously merged, CacheHit+Miss already holds the sum and
	// PromptTokens may still be the first attempt's value — prefer stored sums.
	if acc.CacheHitTokens+acc.CacheMissTokens > 0 {
		ah, am = acc.CacheHitTokens, acc.CacheMissTokens
	}
	merged.CacheHitTokens = ah + bh
	merged.CacheMissTokens = am + bm
	merged.CacheWriteTokens += attempt.CacheWriteTokens
	merged.CacheWriteBilledTokens += attempt.CacheWriteBilledTokens
	merged.PromptTokens = billablePrompt(merged.CacheHitTokens, merged.CacheMissTokens, 0)
	if merged.PromptTokens == 0 {
		merged.PromptTokens = acc.PromptTokens + attempt.PromptTokens
	}
	merged.CompletionTokens += attempt.CompletionTokens
	merged.ReasoningTokens += attempt.ReasoningTokens
	merged.TotalTokens += usageTotalTokens(attempt)
	merged.RequestCount = usageRequestCount(acc) + usageRequestCount(attempt)
	if attempt.Estimated {
		merged.Estimated = true
	}
	if attempt.FinishReason != "" {
		merged.FinishReason = attempt.FinishReason
	}
	return &merged
}

// storeLatestRequestUsage records single-request usage, never a billable aggregate.
func (a *Agent) storeLatestRequestUsage(attempt *provider.Usage) {
	if a == nil || attempt == nil {
		return
	}
	// Skip request-only shells with no token shape.
	if attempt.PromptTokens <= 0 && attempt.CompletionTokens <= 0 && attempt.TotalTokens <= 0 {
		return
	}
	clone := *attempt
	// Keep the per-attempt RequestCount; context calculations do not use it.
	a.lastUsage.Store(&clone)
	a.setPromptTokenCalibrationFromUsage(&clone)
}

// finalizeSamplingUsage builds the Usage event payload for consumers that
// expect one coherent billable record:
//   - PromptTokens / cache hit+miss / Completion / Total / RequestCount: billable aggregate
//   - Context* fields: latest attempt only (context gauges + rebind telemetry)
func finalizeSamplingUsage(billable, latest *provider.Usage) *provider.Usage {
	if billable == nil && latest == nil {
		return nil
	}
	if billable == nil {
		out := *latest
		applyLatestContextShape(&out, latest)
		return &out
	}
	out := *billable
	if latest != nil {
		applyLatestContextShape(&out, latest)
		out.FinishReason = latest.FinishReason
	}
	// Ensure PromptTokens matches billable input (hit+miss) for CLI/ACP/Desktop
	// telemetry that requires cache totals to align with PromptTokens.
	if hitMiss := out.CacheHitTokens + out.CacheMissTokens; hitMiss > 0 {
		out.PromptTokens = hitMiss
	}
	if out.TotalTokens < out.PromptTokens+out.CompletionTokens {
		out.TotalTokens = out.PromptTokens + out.CompletionTokens
	}
	return &out
}

// mergeStreamUsage remains for missing-reasoning style single-repair merges that
// need a simple sum. Sampling recovery uses mergeSamplingUsage instead.
func mergeStreamUsage(first, retry *provider.Usage) *provider.Usage {
	return mergeSamplingUsage(first, retry)
}

func usageTotalTokens(u *provider.Usage) int {
	if u == nil {
		return 0
	}
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.PromptTokens + u.CompletionTokens
}

func usageRequestCount(usage *provider.Usage) int {
	if usage == nil {
		return 0
	}
	if usage.RequestCount > 0 {
		return usage.RequestCount
	}
	return 1
}

func (a *Agent) emitTurnUsage(usage *provider.Usage, cacheDiagnostics *CacheDiagnostics) {
	if usage == nil || (usage.TotalTokens <= 0 && usage.RequestCount <= 0) {
		return
	}
	// lastUsage must stay as the latest single-request shape (set during
	// sampling recovery). Never overwrite it with a multi-attempt billable
	// aggregate — that would inflate ContextSnapshot and compaction decisions.
	if a.lastUsage.Load() == nil && usage.PromptTokens > 0 {
		a.storeLatestRequestUsage(usage)
	}
	a.sink.Emit(event.Event{Kind: event.Usage, ModelRef: a.modelRef, Usage: usage, Pricing: a.pricing,
		UsageSource:      a.usageSource,
		CacheDiagnostics: cacheDiagnostics,
		SessionHit:       int(a.sessCacheHit.Load()), SessionMiss: int(a.sessCacheMiss.Load())})
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
			_, _ = ctrl.ConsumeFinalization(a.recoveryTaskID)
		}
		return false, &RecoveryPauseError{
			Message:    "Automatic retries paused. Reasonix stopped repeated attempts and kept completed work. Send \"continue\" to start a fresh attempt, or add instructions to change direction.",
			StopReason: reason,
		}
	}
	readiness := a.finalReadinessCheckFor()
	if state.graceRound && (readiness.reason != "" || !hasVisibleFinalAnswer(text)) {
		a.contextManager().ObserveUsage(usage)
		return false, &maxStepsPause{steps: state.runMaxSteps, key: state.runMaxStepsKey}
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
	a.emitTurnShadows(state.input)
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
		a.contextManager().ObserveUsage(usage)
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
		msg := provider.Message{
			Role:       provider.RoleTool,
			Content:    results[i],
			Images:     images[i],
			ToolCallID: call.ID,
			Name:       call.Name,
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
	a.contextManager().ObserveUsage(usage)

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
