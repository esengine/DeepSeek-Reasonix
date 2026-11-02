package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/delivery"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/safety/permission"
)

// barrierLevel is how far an earlier failure reaches: a failed modification may
// leave the workspace half-applied and stops everything, while a call stopped
// before it ran changed nothing and stops only a check.
type barrierLevel int32

const (
	barrierNone barrierLevel = iota
	barrierVerificationOnly
	barrierFull
)

func (l barrierLevel) covers(mutates, verification bool) bool {
	switch l {
	case barrierFull:
		return mutates || verification
	case barrierVerificationOnly:
		return verification
	}
	return false
}

// Refusal identities of the calls the host stops before they run, so a window
// can say why in the reader's language while the model keeps the English text.
const (
	CodeBatchSkippedAfterFailedChange = "batch.skipped_after_failed_change"
	CodeBatchCheckAfterStoppedCall    = "batch.check_after_stopped_call"
)

func (l barrierLevel) refusalCode() string {
	if l == barrierVerificationOnly {
		return CodeBatchCheckAfterStoppedCall
	}
	return CodeBatchSkippedAfterFailedChange
}

// message says which of the two happened: told a modification failed when none
// ran, a model goes looking for a write it never made.
func (l barrierLevel) message() string {
	if l == barrierVerificationOnly {
		return "blocked: an earlier call in this batch was stopped before it ran, so this check would not " +
			"measure the state you asked for. Re-run the stopped call first, or run this check on its own."
	}
	return "blocked: skipped because an earlier modification in this tool batch failed or was blocked. " +
		"Fix or re-run the failed change first; verification was not executed."
}

// toolOutcome is one tool call's result. output is the first-visible bounded
// form the model sees; rawOutput is the full original when truncation applied
// (empty when identical so we avoid double storage). images ride outside text.
type toolOutcome struct {
	preview   *event.FileDiff
	output    string
	rawOutput string // full original when different from output
	unbounded string // what the tool returned, whenever bound moved or cut it
	images    []string
	videos    []provider.VideoRef
	blocked   bool
	// endsRound marks a call whose result is a user decision. The pre-scheduling
	// scan cannot see one an extension substituted in, so the call reports it.
	endsRound     bool
	errMsg        string
	checkpointErr error
	// refusalCode is the identity of a host refusal, carried beside the words
	// rather than recovered from them. Empty when the call was not refused.
	refusalCode    string
	workspaceLease *event.WorkspaceLease
	bound          event.OutputBound
	truncMsg       string
	// provenance is where the result's content came from, read after the call
	// ran; only the provider-bound message is labelled with it.
	provenance   tool.Provenance
	resolved     bool
	resolvedName string
	capabilityID string
	// resolvedProfile is the delegation behind a proxy call: the model only
	// ever sees use_capability, so without this a dispatched sub-agent reaches
	// the frontend anonymous.
	resolvedProfile            *event.Profile
	resolvedReadOnly, executed bool
	workspaceMutation          *event.WorkspaceMutation
	effective                  workspaceEffectiveCall
	// execution is local shell metadata (optional). Provider messages strip it
	// via ModelMessages; UI/event sinks surface it on ToolResult cards.
	execution *tool.ShellExecution
	// todoEcho marks a todo_write that wrote the list the host already held.
	todoEcho bool
}

// keepFull records what the bound replaced: rawOutput for the conversation when
// it cut the middle, unbounded for a nested caller whenever it changed anything.
func (o *toolOutcome) keepFull(full string) {
	full = secrets.Masker().String(full)
	if o.truncMsg != "" {
		o.rawOutput = full
	}
	if o.bound.Kind != event.BoundWhole {
		o.unbounded = full
	}
}

// refusedShellExecution describes a shell call the host stopped before launch,
// so a refusal carries the metadata a failure would and stays visible to tool
// cards and trajectory digests. phase names the gate that stopped it.
func refusedShellExecution(t tool.Tool, args json.RawMessage, phase string) *tool.ShellExecution {
	ex := &tool.ShellExecution{Kind: "shell"}
	if bt, ok := t.(tool.DetailedExecutor); ok {
		if desc := bt.ExecutionDescriptor(args); desc != nil {
			ex = desc
		}
	}
	ex.State = tool.ShellStateNotRun
	ex.FailurePhase = phase
	ex.MutationRisk = tool.ShellMutationNotStarted
	ex.Verification = tool.ShellVerificationNotVerification
	return ex
}

// shellRefusal is refusedShellExecution for gates that run before the call is
// known to be a shell: only a tool that can describe an execution is one.
func shellRefusal(t tool.Tool, args json.RawMessage, phase string) *tool.ShellExecution {
	if _, ok := t.(tool.DetailedExecutor); !ok {
		return nil
	}
	return refusedShellExecution(t, args, phase)
}

// batchExecution is the result of one provider tool-call batch.
type batchExecution struct {
	results    []string
	outcomes   []toolOutcome
	images     [][]string
	videos     [][]provider.VideoRef
	executions []*tool.ShellExecution
}

// executeBatch dispatches one model turn's tool calls. ToolDispatch events are
// emitted up front in call order; contiguous known ReadOnly calls fan out
// across goroutines while unknown and writer calls run serially so write/read
// ordering stays provider-ordered. ToolResult events are emitted after the
// batch in call order. Images are aligned by index with results.
func (a *Agent) executeBatch(ctx context.Context, turn *turnRuntime, calls []provider.ToolCall) batchExecution {
	// Session owns the original slice; dependent previews and argument preparation
	// must not mutate it outside the session lock.
	calls = a.prepareCallBatch(calls)
	for _, c := range calls {
		a.emitEagerToolDispatch(ctx, c)
	}

	results := make([]string, len(calls))
	outcomes := make([]toolOutcome, len(calls))
	durations := make([]int64, len(calls))
	startedAt := make([]int64, len(calls))
	completedStepInBatch := false
	receiptMark := a.ledgerMark()
	surfaceWriters := make([]bool, len(calls))
	run := func(i int) {
		t, _, ambiguous := a.svc.tools.ResolveCall(calls[i].Name)
		known := t != nil && len(ambiguous) == 0
		writer := known && !t.ReadOnly()
		surfaceWriters[i] = writer
		start := time.Now()
		startedAt[i] = start.UnixMilli()
		if calls[i].Name == "complete_step" && completedStepInBatch {
			output := "blocked: only one successful complete_step is allowed per tool-call round. Continue from the newly promoted in_progress todo in the next round instead of batching sign-offs."
			outcomes[i] = toolOutcome{output: output, blocked: true, errMsg: "blocked: complete_step sign-offs must be serial"}
			if a.task.ledger != nil {
				a.task.ledger.Record(evidence.ReceiptFromToolCall(calls[i].Name, json.RawMessage(calls[i].ExecutionArguments()), false, evidence.ToolFacts{ReadOnly: true}))
			}
			durations[i] = time.Since(start).Milliseconds()
			results[i] = output
			return
		}
		outcomes[i] = a.executeOne(ctx, turn, calls[i])
		if p := outcomes[i].preview; p != nil {
			calls[i].Diff, calls[i].Added, calls[i].Removed = p.Diff, p.Added, p.Removed
		}
		if outcomes[i].executed {
			surfaceWriters[i] = outcomes[i].workspaceMutation != nil
		}
		if outcomes[i].resolved {
			readOnly := outcomes[i].resolvedReadOnly
			calls[i].ResolvedName = outcomes[i].resolvedName
			calls[i].CapabilityID = outcomes[i].capabilityID
			calls[i].ResolvedReadOnly = &readOnly
			surfaceWriters[i] = !readOnly
		}
		if calls[i].Name == "complete_step" && outcomes[i].errMsg == "" {
			completedStepInBatch = true
		}
		durations[i] = time.Since(start).Milliseconds()
		results[i] = outcomes[i].output
	}
	finalize := func(i int) {
		if calls[i].ResolvedReadOnly != nil {
			a.sess.conversation.UpdateToolCallResolution(calls[i])
			a.emitResolvedToolDispatch(calls[i], outcomes[i].resolvedProfile)
		}
	}
	cancelled := false
	markCancelled := func(start int) {
		markCancelledFrom(ctx, start, calls, results, outcomes)
		cancelled = true
	}

	// Deterministic dependency barrier: after a mutating call fails or is
	// blocked, later mutations/verifications in the batch are skipped; read-only
	// diagnosis still runs. executeOne re-checks after proxy resolution.
	mutationBatchStop := false
	a.mutationDependencyBarrier.Store(int32(barrierNone))
	markDependencySkipped := func(start int, level barrierLevel) {
		a.mutationDependencyBarrier.Store(int32(level))
		for j := start; j < len(calls); j++ {
			if results[j] != "" {
				continue
			}
			if out, ok := a.barrierSkip(calls[j], level); ok {
				results[j], outcomes[j], durations[j] = out.output, out, 0
			}
		}
		mutationBatchStop = true
	}

	scheduled, barrier := scheduleUpToDecision(a.svc.tools, calls)

	for _, batch := range partitionToolCalls(ctx, a.svc.tools, scheduled) {
		if ctx.Err() != nil {
			markCancelled(batch.start)
			break
		}
		if batch.parallel && batch.end-batch.start > 1 {
			// Parallel segments are read-only by construction; no mutation barrier.
			ranUntil := runParallel(ctx, batch.start, batch.end, run)
			for i := batch.start; i < ranUntil; i++ {
				finalize(i)
			}
			// After parallel execution completes, check if context was cancelled.
			// The individual tool executions should have detected ctx.Done(), but
			// we verify here to ensure we don't continue to subsequent batches.
			if ctx.Err() != nil {
				markCancelled(ranUntil)
				break
			}
			continue
		}
		for i := batch.start; i < batch.end; i++ {
			// Before executing the next tool, check if context was cancelled.
			// This prevents starting new tools when a previous tool's execution
			// triggered cancellation.
			if ctx.Err() != nil {
				markCancelled(i)
				break
			}
			if mutationBatchStop {
				// Fill dependency skips for remaining mutating/verify calls, then
				// allow any residual read-only diagnosis to run individually.
				if results[i] != "" {
					continue
				}
				t, _, ambiguous := a.svc.tools.ResolveCall(calls[i].Name)
				known := t != nil && len(ambiguous) == 0
				readOnly := known && t.ReadOnly()
				if calls[i].Name == "bash" && permission.BashCommandIsReadOnly(json.RawMessage(calls[i].ExecutionArguments())) {
					readOnly = true
				}
				isVerification := calls[i].Name == "bash" && evidence.IsDeliveryVerificationCommand(bashCommandFromArgs(json.RawMessage(calls[i].ExecutionArguments())))
				mutates := evidence.ToolCallMutates(calls[i].Name, json.RawMessage(calls[i].ExecutionArguments()), readOnly)
				if mutates || isVerification {
					markDependencySkipped(i, barrierFull)
					// markDependencySkipped fills this index; move on.
					if results[i] != "" {
						continue
					}
				}
			}
			if results[i] != "" {
				// Pre-filled dependency skip.
				finalize(i)
				continue
			}
			run(i)
			finalize(i)
			if outcomes[i].endsRound { // a barrier substituted in after the scan
				markDeferredAfterDecision(i+1, calls, results, outcomes, durations)
				cancelled = true
				break
			}
			// Mutation/verification failure barrier for the rest of this batch.
			if level := batchFailureBarrier(a, calls[i], outcomes[i]); level != barrierNone {
				mutationBatchStop = true
				markDependencySkipped(i+1, level)
			}
			// After each tool execution, also check if the context was cancelled.
			// If so, stop executing remaining tools and return immediately so
			// the agent loop can detect the cancellation and exit.
			if ctx.Err() != nil {
				markCancelled(i + 1)
				break
			}
		}
		if cancelled {
			break
		}
	}
	markDeferredAfterDecision(barrier+1, calls, results, outcomes, durations)
	maskResults(results, outcomes)

	for i, c := range calls {
		o := outcomes[i]
		t, _, ambiguous := a.svc.tools.ResolveCall(c.Name)
		ok := t != nil && len(ambiguous) == 0
		readOnly := ok && t.ReadOnly()
		if c.ResolvedReadOnly != nil {
			readOnly = *c.ResolvedReadOnly
		}
		tr := event.Tool{
			ID:                    c.ID,
			Name:                  c.Name,
			Args:                  c.ExecutionArguments(),
			ArgumentNormalization: c.ArgumentNormalization,
			ResolvedName:          c.ResolvedName,
			CapabilityID:          c.CapabilityID,
			Output:                o.output, Images: o.images, Videos: shownVideos(o.videos, a.nativeVideoModel()),
			Err: o.errMsg, RefusalCode: o.refusalCode, WorkspaceLease: o.workspaceLease,
			ReadOnly:   readOnly,
			Bound:      o.bound,
			DurationMs: durations[i],
			Execution:  toEventShellExecution(o.execution, durations[i]),
			Issuer:     event.IssuedByModel, Executed: o.executed,
		}
		manifest := o.output
		if o.rawOutput != "" {
			manifest = o.rawOutput
		}
		tr.Deliveries = delivery.Decode(c.ResolvedName, c.CapabilityID, o.errMsg != "" || !o.executed || o.blocked, manifest)
		if startedAt[i] > 0 {
			tr.StartedAt = startedAt[i]
			tr.EndedAt = startedAt[i] + durations[i]
			attachWorkspaceMutation(&tr, o.workspaceMutation)
		}
		a.emitToolCard(ctx, c, tr, o.todoEcho)
		a.emitToolOutcomeWarnings(o)
	}
	a.observeOutcomeShadow(cancelled, receiptMark)
	images := make([][]string, len(calls))
	videos := make([][]provider.VideoRef, len(calls))
	executions := make([]*tool.ShellExecution, len(calls))
	for i := range outcomes {
		images[i] = outcomes[i].images
		videos[i] = outcomes[i].videos
		executions[i] = outcomes[i].execution
	}
	return batchExecution{
		results:    results,
		outcomes:   outcomes,
		images:     images,
		videos:     videos,
		executions: executions,
	}
}

// barrierSkip pre-fills one call the barrier covers. It answers false for a
// proxy or an ambiguous target, which fall through to run() so executeOne can
// resolve the real one and re-apply the barrier before Commit/Execute.
func (a *Agent) barrierSkip(call provider.ToolCall, level barrierLevel) (toolOutcome, bool) {
	isVerification := call.Name == "bash" && evidence.IsDeliveryVerificationCommand(bashCommandFromArgs(json.RawMessage(call.ExecutionArguments())))
	if !batchCallStaticallySkippable(a, call) || !level.covers(true, isVerification) {
		return toolOutcome{}, false
	}
	msg := level.message()
	var ex *tool.ShellExecution
	if call.Name == "bash" {
		resolved, _, _ := a.svc.tools.ResolveCall(call.Name)
		ex = refusedShellExecution(resolved, json.RawMessage(call.ExecutionArguments()), tool.ShellPhaseDependency)
		if isVerification {
			ex.Verification = tool.ShellVerificationNotRun
		}
	}
	return toolOutcome{output: msg, blocked: true, errMsg: firstLine(msg), refusalCode: level.refusalCode(), execution: ex}, true
}

// batchFailureBarrier reports how far a finished call's failure reaches: a
// mutation that failed or was blocked stops later mutations and verifications,
// one stopped before it ran stops only verification.
func batchFailureBarrier(a *Agent, call provider.ToolCall, o toolOutcome) barrierLevel {
	if o.errMsg == "" && !o.blocked {
		return barrierNone
	}
	readOnly := false
	toolName := call.Name
	toolArgs := json.RawMessage(call.ExecutionArguments())
	t, _, ambiguous := a.svc.tools.ResolveCall(call.Name)
	known := t != nil && len(ambiguous) == 0
	if known {
		readOnly = t.ReadOnly()
	}
	if call.ResolvedReadOnly != nil {
		readOnly = *call.ResolvedReadOnly
	}
	if o.resolved {
		readOnly = o.resolvedReadOnly
	}
	if o.effective.name != "" {
		toolName = o.effective.name
		toolArgs = o.effective.args
		readOnly = o.effective.readOnly
	}
	if toolName == "bash" && permission.BashCommandIsReadOnly(toolArgs) {
		readOnly = true
	}
	// Verification failures do not open the dependency barrier by themselves —
	// only a failed modification does.
	if toolName == "bash" && evidence.IsDeliveryVerificationCommand(bashCommandFromArgs(toolArgs)) {
		return barrierNone
	}
	// Resolved writers (including MCP targets behind use_capability) count even
	// when the provider-visible proxy advertised ReadOnly.
	if o.resolved && !o.resolvedReadOnly {
		return barrierFull
	}
	// Fail closed only for a target the host could not classify: a blanket
	// !readOnly fallback would re-admit the meta tools ToolCallMutates exempts,
	// letting a failed todo_write block every real edit left in the batch.
	if known && !evidence.ToolCallMutates(toolName, toolArgs, readOnly) {
		return barrierNone
	}
	// Stopped before it ran, and not a writer the host could name: it changed
	// nothing, so later edits work from a known state. The check still cannot.
	if o.execution != nil && o.execution.MutationRisk == tool.ShellMutationNotStarted &&
		evidence.ToolCallMutationClass(toolName, toolArgs, readOnly) != evidence.MutationProven {
		return barrierVerificationOnly
	}
	return barrierFull
}

// batchCallStaticallySkippable reports whether a remaining call can be marked
// not_run/dependency without resolving a proxy. Proxies and unknown tools
// return false so executeOne can resolve the real target first.
func batchCallStaticallySkippable(a *Agent, call provider.ToolCall) bool {
	t, _, ambiguous := a.svc.tools.ResolveCall(call.Name)
	if t == nil || len(ambiguous) > 0 {
		// Unknown / ambiguous: fail closed via executeOne path.
		return false
	}
	// A proxy may resolve against a live capability whose result can change
	// between calls, so never resolve here just to pre-fill a skip: executeOne
	// resolves exactly once and classifies the real target before Commit.
	if _, ok := t.(tool.CallResolver); ok {
		return false
	}
	// Delegation spawns work rather than changing state, so a failed one does
	// not open the barrier; being skipped by it is the same question, and a
	// bare !ReadOnly answers it the other way.
	if evidence.IsNonMutationMetaTool(call.Name) {
		return false
	}
	readOnly := t.ReadOnly()
	if call.Name == "bash" && permission.BashCommandIsReadOnly(json.RawMessage(call.ExecutionArguments())) {
		readOnly = true
	}
	isVerification := call.Name == "bash" && evidence.IsDeliveryVerificationCommand(bashCommandFromArgs(json.RawMessage(call.ExecutionArguments())))
	if isVerification {
		return true
	}
	return !readOnly || evidence.ToolCallMutates(call.Name, json.RawMessage(call.ExecutionArguments()), readOnly)
}

type toolCallBatch struct {
	start    int
	end      int
	parallel bool
}

// partitionToolCalls keeps provider order while letting contiguous read-only
// tools run together. A writer, an unresolvable name, and a tool that declares
// it needs its own place each get a single-call serial batch.
func partitionToolCalls(ctx context.Context, r *tool.Registry, calls []provider.ToolCall) []toolCallBatch {
	var batches []toolCallBatch
	for i := 0; i < len(calls); {
		if parallelisable(ctx, r, calls[i]) {
			start := i
			i++
			for i < len(calls) && parallelisable(ctx, r, calls[i]) {
				i++
			}
			batches = append(batches, toolCallBatch{start: start, end: i, parallel: true})
			continue
		}
		batches = append(batches, toolCallBatch{start: i, end: i + 1})
		i++
	}
	return batches
}

func parallelisable(ctx context.Context, r *tool.Registry, call provider.ToolCall) bool {
	t, canonical, ambiguous := r.ResolveCall(call.Name)
	if t == nil || len(ambiguous) > 0 {
		return false
	}
	args := json.RawMessage(call.ExecutionArguments())
	// ReadOnly says a call needs no approval; it does not say the call may share
	// a batch. A tool that advances host state or reads what an earlier call in
	// this reply started answers that second question itself.
	if tool.RunsSequentially(ctx, t, args) {
		return false
	}
	if t.ReadOnly() {
		return true
	}
	// Bash is writer-capable in its schema, so it never joined a parallel run
	// even when its arguments read as read-only — the same fact permission,
	// mutation accounting, and evidence already act on.
	return canonical == "bash" && permission.BashCommandIsReadOnly(args)
}

func runParallel(ctx context.Context, start, end int, run func(int)) int {
	const maxParallel = 8
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	ranUntil := start
launch:
	for i := start; i < end; i++ {
		if ctx.Err() != nil {
			break
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break launch
		}
		if ctx.Err() != nil {
			<-sem
			break
		}

		wg.Add(1)
		ranUntil = i + 1
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			run(i)
		}()
	}
	wg.Wait()
	return ranUntil
}

// scheduleUpToDecision cuts the batch at the first call whose result is a
// decision only the user can give, and returns that index. Scanning happens
// before scheduling: by the time a barrier executes, its parallel neighbours
// already have.
func scheduleUpToDecision(reg *tool.Registry, calls []provider.ToolCall) ([]provider.ToolCall, int) {
	for i, c := range calls {
		t, _, ambiguous := reg.ResolveCall(c.Name)
		if t != nil && len(ambiguous) == 0 && tool.IsDecisionBarrierCall(t, json.RawMessage(c.ExecutionArguments())) {
			return calls[:i+1], i
		}
	}
	return calls, len(calls)
}

// markDeferredAfterDecision closes out the calls a barrier ended the round for.
// They were authored before the answer existed, so they reason from an answer
// the model had not received — read-only ones included.
func markDeferredAfterDecision(start int, calls []provider.ToolCall, results []string, outcomes []toolOutcome, durations []int64) {
	const msg = "not run: this round stopped at a question for the user. It was written before their answer " +
		"existed, so it is not carried over — read the answer, then decide what to do next."
	for j := start; j < len(calls); j++ {
		if results[j] != "" {
			continue
		}
		results[j] = msg
		outcomes[j] = toolOutcome{output: msg, blocked: true, errMsg: "not run: stopped at a user decision"}
		durations[j] = 0
	}
}

// markCancelledFrom closes out the calls a cancellation reached before they ran.
func markCancelledFrom(ctx context.Context, start int, calls []provider.ToolCall, results []string, outcomes []toolOutcome) {
	errMsg := context.Canceled.Error()
	if err := ctx.Err(); err != nil {
		errMsg = err.Error()
	}
	const output = "cancelled: context cancelled before execution"
	for j := start; j < len(calls); j++ {
		results[j] = output
		outcomes[j] = toolOutcome{output: output, errMsg: errMsg}
	}
}

// refusalCodeOf is the identity a tool's own refusal carries, so a frontend
// names the cause the tool decided rather than reading it out of the sentence.
func refusalCodeOf(err error) string {
	var refusal tool.Refusal
	if errors.As(err, &refusal) {
		return refusal.Code
	}
	var identity tool.RefusalIdentity
	if errors.As(err, &identity) {
		return identity.RefusalCode()
	}
	return ""
}

func (a *Agent) emitToolOutcomeWarnings(outcome toolOutcome) {
	a.emitCheckpointWarning(outcome.checkpointErr)
	if outcome.truncMsg != "" {
		a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: outcome.truncMsg})
	}
}
