package agent

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// mutationBarrierCause is an immutable, argument-free description of the
// first durable-state write that failed or was blocked in a tool batch.
type mutationBarrierCause struct {
	callID                string
	toolName              string
	stateMutation         bool
	workspaceMutation     bool
	contentMutation       bool
	repositoryMutation    bool
	classificationKnown   bool
	reason, blockingPhase string
}

func (c *mutationBarrierCause) message() string {
	if c == nil {
		return "blocked: skipped because an earlier modification failed or was blocked in this tool batch. " +
			"Fix or re-run the failed change first; verification was not executed."
	}
	reason := c.reason
	if reason == "" {
		reason = "state mutation whose effects cannot be proven read-only"
	}
	action := "failed"
	if c.blockingPhase == "blocked" {
		action = "was blocked"
	}
	return "blocked: skipped because an earlier modification (" + reason + ") " + action + " in this tool batch. " +
		"Fix or re-run the failed change first; verification was not executed."
}

// toolOutcome is one tool call's result. output is the first-visible bounded
// form the model sees; rawOutput is the full original when truncation applied
// (empty when identical so we avoid double storage). images ride outside text.
type toolOutcome struct {
	output                     string
	rawOutput                  string // full original when different from output
	images                     []string
	blocked                    bool
	errMsg                     string
	truncated                  bool
	truncMsg                   string
	resolved                   bool
	resolvedName               string
	capabilityID               string
	resolvedReadOnly, executed bool
	workspaceMutation          *event.WorkspaceMutation
	effective                  workspaceEffectiveCall
	// execution is local shell metadata (optional). Provider messages strip it
	// via ModelMessages; UI/event sinks surface it on ToolResult cards.
	execution *tool.ShellExecution
	// recoveryGeneration is the gate generation captured before execution so
	// ObserveResult can ignore stale results after a mode switch.
	recoveryGeneration uint64
	// recoveryStopTurn is set when Auto Episode budgets are exhausted.
	recoveryStopTurn   bool
	recoveryStopReason string
}

// batchExecution is the result of one provider tool-call batch.
type batchExecution struct {
	results            []string
	outcomes           []toolOutcome
	images             [][]string
	executions         []*tool.ShellExecution
	recoveryStopTurn   bool
	recoveryStopReason string
}

// executeBatch dispatches one model turn's tool calls. ToolDispatch events are
// emitted up front in call order; contiguous known ReadOnly calls fan out
// across goroutines while unknown and writer calls run serially so write/read
// ordering stays provider-ordered. ToolResult events are emitted after the
// batch in call order. Images are aligned by index with results.
func (a *Agent) executeBatch(ctx context.Context, turn *turnRuntime, calls []provider.ToolCall) batchExecution {
	// The assistant message already stored this slice in Session. Keep execution
	// state separate so refreshing a dependent preview never mutates shared
	// session memory outside Session's lock.
	calls = append([]provider.ToolCall(nil), calls...)
	for _, c := range calls {
		a.emitFullToolDispatch(ctx, c, false)
	}

	results := make([]string, len(calls))
	outcomes := make([]toolOutcome, len(calls))
	durations := make([]int64, len(calls))
	startedAt := make([]int64, len(calls))
	completedStepInBatch := false
	// Snapshot the receipt count before the batch runs: if a loop guard fires
	// for this batch, successes recorded during it (a mixed batch where only one
	// call was guard-blocked) must already count as progress against the pass.
	receiptMark := 0
	if a.task.ledger != nil {
		receiptMark = a.task.ledger.Len()
	}
	// Full dispatches used the batch's initial file state. After a writer runs
	// (even a failed one — disk may have mutated), refresh dependent writer
	// previews. The first writer stays on the single-preview fast path.
	earlierWriterRan := false
	surfaceWriters := make([]bool, len(calls))
	run := func(i int) {
		t, _, ambiguous := a.svc.tools.ResolveCall(calls[i].Name)
		known := t != nil && len(ambiguous) == 0
		writer := known && !t.ReadOnly()
		surfaceWriters[i] = writer
		if earlierWriterRan && writer {
			if refreshed, changed := refreshCurrentFileDiff(ctx, t, calls[i]); changed {
				calls[i] = refreshed
				a.sess.conversation.UpdateToolCallPreview(refreshed)
				a.emitFullToolDispatch(ctx, refreshed, true)
			}
		}
		start := time.Now()
		startedAt[i] = start.UnixMilli()
		if calls[i].Name == "complete_step" && completedStepInBatch {
			output := "blocked: only one successful complete_step is allowed per tool-call round. Continue from the newly promoted in_progress todo in the next round instead of batching sign-offs."
			outcomes[i] = toolOutcome{output: output, blocked: true, errMsg: "blocked: complete_step sign-offs must be serial"}
			if a.task.ledger != nil {
				a.task.ledger.Record(evidence.ReceiptFromToolCall(calls[i].Name, json.RawMessage(calls[i].Arguments), false, true))
			}
			durations[i] = time.Since(start).Milliseconds()
			results[i] = output
			return
		}
		outcomes[i] = a.executeOne(ctx, turn, calls[i])
		recordWorkspaceMutation(a.svc.sink, outcomes[i].workspaceMutation)
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
			a.emitResolvedToolDispatch(calls[i])
		}
		if surfaceWriters[i] || (outcomes[i].resolved && !outcomes[i].resolvedReadOnly) {
			earlierWriterRan = true
		}
	}
	cancelled := false
	markCancelled := func(start int) {
		errMsg := context.Canceled.Error()
		if err := ctx.Err(); err != nil {
			errMsg = err.Error()
		}
		output := "cancelled: context cancelled before execution"
		for j := start; j < len(calls); j++ {
			results[j] = output
			outcomes[j] = toolOutcome{output: output, errMsg: errMsg}
		}
		cancelled = true
	}

	// recoveryBatchStop blocks remaining tools after Episode budgets are
	// exhausted so tool-call / result pairs stay complete for the provider.
	recoveryBatchStop := false
	recoveryStopReason := ""
	markRecoveryStopped := func(start int, reason string) {
		msg := "blocked: Auto recovery paused this turn; do not call more tools. Summarize completed work for the user."
		for j := start; j < len(calls); j++ {
			if results[j] != "" {
				continue
			}
			results[j] = msg
			outcomes[j] = toolOutcome{
				output:             msg,
				blocked:            true,
				errMsg:             firstLine(msg),
				recoveryStopTurn:   true,
				recoveryStopReason: reason,
			}
		}
		recoveryBatchStop = true
		if reason != "" {
			recoveryStopReason = reason
		}
	}

	// Deterministic dependency barrier: after a mutating call fails or is
	// blocked, later mutations/verifications in the batch are skipped; read-only
	// diagnosis still runs. executeOne re-checks after proxy resolution.
	mutationBatchStop := false
	a.mutationDependencyBarrier.Store(nil)
	markDependencySkipped := func(start int, cause *mutationBarrierCause) {
		if cause != nil {
			a.mutationDependencyBarrier.CompareAndSwap(nil, cause)
		}
		cause = a.mutationDependencyBarrier.Load()
		for j := start; j < len(calls); j++ {
			if results[j] != "" {
				continue
			}
			// Pre-classify when statically certain. Proxies and ambiguous
			// targets fall through to run() so executeOne can resolve the real
			// target and re-apply the barrier before Commit/Execute.
			if !batchCallStaticallySkippable(a, calls[j]) {
				continue
			}
			isVerification := calls[j].Name == "bash" && evidence.IsDeliveryVerificationCommand(bashCommandFromArgs(json.RawMessage(calls[j].Arguments)))
			msg := cause.message()
			var ex *tool.ShellExecution
			if calls[j].Name == "bash" {
				ex = &tool.ShellExecution{
					Kind:         "shell",
					State:        tool.ShellStateNotRun,
					FailurePhase: tool.ShellPhaseDependency,
					MutationRisk: tool.ShellMutationNotStarted,
					Verification: tool.ShellVerificationNotVerification,
				}
				if isVerification {
					ex.Verification = tool.ShellVerificationNotRun
				}
				if t, _, amb := a.svc.tools.ResolveCall(calls[j].Name); t != nil && len(amb) == 0 {
					if bt, ok := t.(tool.DetailedExecutor); ok {
						if desc := bt.ExecutionDescriptor(json.RawMessage(calls[j].Arguments)); desc != nil {
							ex.Shell = desc.Shell
							ex.ShellVersion = desc.ShellVersion
							ex.Platform = desc.Platform
							ex.SupportsAndAnd = desc.SupportsAndAnd
						}
					}
				}
			}
			results[j] = msg
			outcomes[j] = toolOutcome{
				output:    msg,
				blocked:   true,
				errMsg:    firstLine(msg),
				execution: ex,
			}
			durations[j] = 0
		}
		mutationBatchStop = true
	}

	for _, batch := range partitionToolCalls(a.svc.tools, calls) {
		if ctx.Err() != nil {
			markCancelled(batch.start)
			break
		}
		if recoveryBatchStop {
			markRecoveryStopped(batch.start, recoveryStopReason)
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
			for i := batch.start; i < batch.end; i++ {
				if outcomes[i].recoveryStopTurn {
					recoveryBatchStop = true
					recoveryStopReason = outcomes[i].recoveryStopReason
					markRecoveryStopped(batch.end, recoveryStopReason)
					break
				}
			}
			if recoveryBatchStop {
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
			if recoveryBatchStop {
				markRecoveryStopped(i, recoveryStopReason)
				break
			}
			if mutationBatchStop {
				// Fill dependency skips for remaining mutating/verify calls, then
				// allow any residual read-only diagnosis to run individually.
				if results[i] != "" {
					continue
				}
				if batchCallStaticallySkippable(a, calls[i]) {
					markDependencySkipped(i, nil)
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
			if outcomes[i].recoveryStopTurn {
				recoveryBatchStop = true
				recoveryStopReason = outcomes[i].recoveryStopReason
				markRecoveryStopped(i+1, recoveryStopReason)
				break
			}
			// Mutation/verification failure barrier for the rest of this batch.
			if cause := batchCallMutationFailureCause(a, calls[i], outcomes[i]); cause != nil {
				mutationBatchStop = true
				markDependencySkipped(i+1, cause)
			}
			// After each tool execution, also check if the context was cancelled.
			// If so, stop executing remaining tools and return immediately so
			// the agent loop can detect the cancellation and exit.
			if ctx.Err() != nil {
				markCancelled(i + 1)
				break
			}
		}
		if cancelled || recoveryBatchStop {
			break
		}
	}

	for i, c := range calls {
		o := outcomes[i]
		t, _, ambiguous := a.svc.tools.ResolveCall(c.Name)
		ok := t != nil && len(ambiguous) == 0
		readOnly := ok && t.ReadOnly()
		if c.ResolvedReadOnly != nil {
			readOnly = *c.ResolvedReadOnly
		}
		tr := event.Tool{
			ID:           c.ID,
			Name:         c.Name,
			Args:         c.Arguments,
			ResolvedName: c.ResolvedName,
			CapabilityID: c.CapabilityID,
			Output:       o.output,
			Err:          o.errMsg,
			ReadOnly:     readOnly,
			Truncated:    o.truncated,
			DurationMs:   durations[i],
			Execution:    toEventShellExecution(o.execution, durations[i]),
		}
		if startedAt[i] > 0 {
			tr.StartedAt = startedAt[i]
			tr.EndedAt = startedAt[i] + durations[i]
			if mutation := o.workspaceMutation; mutation != nil {
				tr.WorkspaceMutation = true
				tr.WorkspacePaths = append([]string(nil), mutation.Paths...)
				tr.WorkspaceAllPaths = mutation.AllPaths
			}
		}
		a.svc.sink.Emit(event.Event{Kind: event.ToolResult, Tool: tr})
		if o.truncated && o.truncMsg != "" {
			a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: o.truncMsg})
		}
	}
	a.applyBatchGuards(ctx, cancelled, calls, outcomes, results, receiptMark)
	images := make([][]string, len(calls))
	executions := make([]*tool.ShellExecution, len(calls))
	for i := range outcomes {
		images[i] = outcomes[i].images
		executions[i] = outcomes[i].execution
		if outcomes[i].recoveryStopTurn {
			recoveryBatchStop = true
			if outcomes[i].recoveryStopReason != "" {
				recoveryStopReason = outcomes[i].recoveryStopReason
			}
		}
	}
	return batchExecution{
		results:            results,
		outcomes:           outcomes,
		images:             images,
		executions:         executions,
		recoveryStopTurn:   recoveryBatchStop,
		recoveryStopReason: recoveryStopReason,
	}
}

// batchCallMutationFailureCause returns a sanitized effect description when a
// durable-state mutation failed or was blocked. Verification failures alone do
// not open the dependency barrier.
func batchCallMutationFailureCause(a *Agent, call provider.ToolCall, o toolOutcome) *mutationBarrierCause {
	if o.errMsg == "" && !o.blocked {
		return nil
	}
	readOnly := false
	toolName := call.Name
	toolArgs := json.RawMessage(call.Arguments)
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
	effects := evidence.ClassifyToolCall(toolName, toolArgs, readOnly)
	if toolName == "bash" && evidence.IsDeliveryVerificationCommand(bashCommandFromArgs(toolArgs)) && !effects.StateMutation {
		return nil
	}
	if !effects.StateMutation {
		return nil
	}
	phase := "failed"
	if o.blocked {
		phase = "blocked"
	}
	return &mutationBarrierCause{
		callID:              call.ID,
		toolName:            toolName,
		stateMutation:       effects.StateMutation,
		workspaceMutation:   effects.WorkspaceMutation,
		contentMutation:     effects.ContentMutation,
		repositoryMutation:  effects.RepositoryMutation,
		classificationKnown: effects.Known && known,
		reason:              effects.Reason,
		blockingPhase:       phase,
	}
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
	readOnly := t.ReadOnly()
	isVerification := call.Name == "bash" && evidence.IsDeliveryVerificationCommand(bashCommandFromArgs(json.RawMessage(call.Arguments)))
	if isVerification {
		return true
	}
	return evidence.ClassifyToolCall(call.Name, json.RawMessage(call.Arguments), readOnly).StateMutation
}

type toolCallBatch struct {
	start    int
	end      int
	parallel bool
}

// partitionToolCalls keeps provider order while letting contiguous known
// read-only tools run together; unknown and writer tools are single-call
// serial batches. Evidence-ledger tools (complete_step, todo_write, wait,
// bash_output) never join a parallel run so provider order stays receipt
// order; use_capability is serial as it may resolve to a real MCP writer.
func partitionToolCalls(r *tool.Registry, calls []provider.ToolCall) []toolCallBatch {
	var batches []toolCallBatch
	for i := 0; i < len(calls); {
		if parallelisable(r, calls[i].Name) {
			start := i
			i++
			for i < len(calls) && parallelisable(r, calls[i].Name) {
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

func parallelisable(r *tool.Registry, name string) bool {
	switch name {
	case "complete_step", "todo_write", "wait", "bash_output", "use_capability", "compress":
		return false
	}
	t, _, ambiguous := r.ResolveCall(name)
	return t != nil && len(ambiguous) == 0 && t.ReadOnly()
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
