# Turn Interruption Recovery: Implementation Plan

## Problem Statement

Issue #9825 reports that long sessions with high tool density experience frequent
**mid-execution interruptions** rather than graceful turn boundaries:

1. Tools complete execution (verified by git log / file state)
2. Turn is interrupted **after** execution but **before** result is returned to model
3. Model context loses completed work; user sees "failed" turn
4. Recovery block lacks complete arguments, making safe retry impossible

The existing `armFinalizationRound` mechanism works at **turn boundaries** but does
not prevent **mid-tool interruptions** from an external interrupt signal.

## Root Cause Analysis

### Current Flow (Problematic)

```
User input → beginRunTurn → runToolLoop:
  ├─ executeToolBatch
  │  ├─ executeOne(bash) ✓ completes
  │  ├─ store result in a.turn.toolResults
  │  └─ emit result to model ← [INTERRUPT HERE]
  ├─ provider.Request (with tool results)
  └─ [never reached; turn interrupted]
```

The interrupt happens **between tool completion and model ingestion**, so:
- Tool side effects (git commit, file write) are durable
- But the result never enters the conversation
- Recovery block sees "interrupted_tools: bash" with no context

### Why Existing Budget Check Doesn't Help

`run_loop.go:527` checks budget **before starting a tool round**:

```go
if axis, detail := a.task.budget.exceeded(a.taskBudgetLimit(ctx)); axis != "" {
    a.armFinalizationRound(ctx, state, landCause{kind: "task_budget", ...})
    return true, nil
}
```

This works when budget expires between rounds, but:
1. A round may execute multiple tools (batch execution)
2. A single bash call may run for minutes (build, test, large file operations)
3. Budget expires **during** tool execution, not before the round starts
4. External watchdog or timeout signal arrives mid-execution

## Solution Architecture

### Phase 1: Tool-Level Budget Checkpoints (This PR)

Add budget checking **inside** the tool loop, not just at round boundaries.

#### 1.1 Budget Checkpoint Before Each Tool

In `executeToolBatch`, check budget before dispatching each tool:

```go
// internal/agent/tool_executor.go (conceptual location)

func (a *Agent) executeToolBatch(ctx context.Context, calls []provider.ToolCall) error {
    for i, call := range calls {
        // Check budget before each tool, not just before the batch
        if shouldGracefullyStop, cause := a.checkToolBoundaryBudget(ctx); shouldGracefullyStop {
            // Return completed results accumulated so far
            a.returnPartialBatchResults(calls[:i])
            // Persist remaining calls as "not_started"
            a.recordNotStartedTools(calls[i:], cause)
            return &gracefulBudgetStop{cause: cause, completed: i, total: len(calls)}
        }
        
        // Proceed with execution
        if err := a.executeOne(ctx, call); err != nil {
            return err
        }
    }
    return nil
}

func (a *Agent) checkToolBoundaryBudget(ctx context.Context) (stop bool, cause landCause) {
    limit := a.taskBudgetLimit(ctx)
    
    // Check if we're within threshold of hard limit
    if axis, detail := a.task.budget.nearLimit(limit, toolEstimateBuffer); axis != "" {
        return true, landCause{kind: "task_budget_threshold", axis: axis, detail: detail}
    }
    
    return false, landCause{}
}
```

Key insight: `nearLimit` checks if `current + estimatedToolCost >= limit`, not just
`current >= limit`. This prevents starting a tool that will exceed budget mid-execution.

#### 1.2 Estimated Tool Cost Buffer

Add heuristic cost estimates per tool type:

```go
// internal/agent/run_budget.go

type toolCostEstimate struct {
    estimatedSeconds time.Duration
    estimatedTokens  int  // for summarization/compression tools
}

var defaultToolEstimates = map[string]toolCostEstimate{
    "bash":       {estimatedSeconds: 10 * time.Second},
    "edit_file":  {estimatedSeconds: 2 * time.Second},
    "write_file": {estimatedSeconds: 2 * time.Second},
    "read_file":  {estimatedSeconds: 1 * time.Second, estimatedTokens: 4000},
    // MCP tools: conservative default
    "mcp__*":     {estimatedSeconds: 5 * time.Second},
}

func (b *runBudget) nearLimit(limit TaskBudget, estimate toolCostEstimate) (axis, detail string) {
    if limit.Tokens > 0 {
        projected := b.promptTokens + b.outputTokens + estimate.estimatedTokens
        if projected >= limit.Tokens {
            return "token", fmt.Sprintf("projected %d tokens would exceed %d budget", projected, limit.Tokens)
        }
    }
    
    if limit.Wall > 0 {
        projected := b.elapsed() + estimate.estimatedSeconds
        if projected >= limit.Wall {
            return "time", fmt.Sprintf("projected %s would exceed %s budget", projected.Round(time.Second), limit.Wall)
        }
    }
    
    // Cost projection requires knowing the model's pricing, skip for now
    return "", ""
}
```

#### 1.3 Graceful Partial Batch Return

When budget threshold is hit mid-batch:

1. Append completed tool results to conversation immediately
2. Persist remaining not-started calls in recovery state
3. Return assistant message with partial results + notice
4. End turn with `taskBudgetPause`

```go
func (a *Agent) returnPartialBatchResults(completed []provider.ToolCall) error {
    // Collect results for completed calls
    var results []provider.Message
    for _, call := range completed {
        if result, ok := a.turn.toolResults[call.ID]; ok {
            results = append(results, provider.Message{
                Role:       provider.RoleTool,
                ToolCallID: call.ID,
                Name:       call.Name,
                Content:    result.output,
            })
        }
    }
    
    // Append to conversation durably
    return a.sess.conversation.AppendMany(results)
}

func (a *Agent) recordNotStartedTools(remaining []provider.ToolCall, cause landCause) {
    // These go into the recovery block as "not_started_tools"
    // so the model can retry them in the next turn with full arguments
    for _, call := range remaining {
        a.turn.notStartedCalls = append(a.turn.notStartedCalls, provider.ToolCallRecord{
            ID:    call.ID,
            Name:  call.Name,
            Arguments: call.Arguments,
            State: provider.ToolRunPending,
        })
    }
}
```

### Phase 2: Interrupt Signal Handling (Follow-up PR)

Handle external interrupts (watchdog timeout, user cancellation, context cancellation)
gracefully rather than abruptly terminating mid-tool.

#### 2.1 Interrupt Context Wrapper

Wrap tool execution with a deferred cleanup that persists state on interrupt:

```go
func (a *Agent) executeOne(ctx context.Context, call provider.ToolCall) error {
    // Mark tool as "running" before execution
    a.persistToolState(call.ID, provider.ToolRunRunning)
    
    // Defer recovery state persistence on panic/interrupt
    defer func() {
        if r := recover(); r != nil {
            a.persistInterruptedState(call, provider.ToolRunUnknown)
            panic(r)
        }
    }()
    
    // Execute with cancellation awareness
    result, err := a.executeToolWithResult(ctx, call)
    
    if ctx.Err() == context.Canceled || ctx.Err() == context.DeadlineExceeded {
        // Context cancelled during execution
        a.persistInterruptedState(call, provider.ToolRunUnknown)
        return ctx.Err()
    }
    
    if err == nil {
        a.persistToolState(call.ID, provider.ToolRunCompleted)
    }
    
    return err
}
```

#### 2.2 Durable Tool State Checkpoint

Persist tool execution state **before** starting, **during** execution (for long-running
tools like bash), and **after** completion:

```go
func (a *Agent) persistToolState(callID string, state provider.ToolRunState) error {
    // Update in-memory state
    a.turn.toolStates[callID] = state
    
    // Persist to recovery sidecar immediately
    // (not waiting for turn completion)
    return a.writeRecoverySidecar()
}

func (a *Agent) writeRecoverySidecar() error {
    // Write to .reasonix/recovery/<session-id>/<turn-id>.json
    // Desktop and CLI read this on restart to populate recovery state
    sidecar := recoverySnapshot{
        TurnID:    a.recovery.turnID,
        Timestamp: time.Now(),
        ToolStates: a.turn.toolStates,
        CompletedResults: a.turn.toolResults,
    }
    return writeSidecarAtomic(a.recoverySidecarPath(), sidecar)
}
```

This ensures that if the process is killed mid-tool (SIGKILL, OOM, crash), the next
session can read the sidecar and reconstruct which tools were running/completed.

#### 2.3 Bash Progress Streaming

For long-running bash calls, emit progress events so the user sees work happening
(and knows the tool didn't hang):

```go
// internal/tool/builtin/bash.go

func (t *BashTool) Execute(ctx context.Context, args BashArgs) (string, error) {
    cmd := exec.CommandContext(ctx, "bash", "-c", args.Command)
    
    // Stream stdout/stderr with progress markers every 5 seconds
    stdout, stderr := &progressBuffer{}, &progressBuffer{}
    cmd.Stdout = stdout
    cmd.Stderr = stderr
    
    // Start execution
    if err := cmd.Start(); err != nil {
        return "", err
    }
    
    // Emit progress events while running
    done := make(chan error, 1)
    go func() { done <- cmd.Wait() }()
    
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case err := <-done:
            // Command finished
            return formatBashResult(stdout, stderr, err), err
        case <-ticker.C:
            // Emit progress event
            event.Emit(ctx, event.Event{
                Kind: event.ToolProgress,
                ToolName: "bash",
                Progress: fmt.Sprintf("running... %d bytes output", stdout.Len()),
            })
        case <-ctx.Done():
            // Context cancelled; kill process gracefully
            cmd.Process.Signal(os.Interrupt)
            time.AfterFunc(2*time.Second, func() { cmd.Process.Kill() })
            return formatBashResult(stdout, stderr, ctx.Err()), ctx.Err()
        }
    }
}
```

### Phase 3: Completed Result Persistence (Follow-up PR)

When a tool completes but the turn is interrupted before the model ingests the result:

1. Persist completed results in the recovery handoff (already implemented in current branch)
2. On next turn, inject results as fenced local-only messages
3. Model sees results in recovery context without re-execution

This is partially implemented via `WriteRecoveryCheck` for file writes. Extend to
all tool results:

```go
// internal/agent/interrupted_recovery.go

func (a *Agent) injectCompletedResultsOnRecovery(r *provider.InterruptedTurnRecovery) error {
    if r == nil || len(r.CompletedTools) == 0 {
        return nil
    }
    
    // Find stored results from prior turn
    priorResults := a.loadCompletedResultsFromSidecar(r.TurnID)
    
    var injected []provider.Message
    for _, summary := range r.CompletedTools {
        if result, ok := priorResults[summary.ID]; ok {
            injected = append(injected, provider.Message{
                Role:       provider.RoleTool,
                ToolCallID: summary.ID,
                Name:       summary.Name,
                Content:    result.Output,
                LocalOnly:  true,  // Not sent to provider
                RecoveryEvidence: true,  // Marked as recovery context
            })
        }
    }
    
    if len(injected) > 0 {
        // Insert before the recovery block
        return a.sess.conversation.InsertBeforeRecovery(injected)
    }
    return nil
}
```

## Testing Strategy

### Unit Tests

- `internal/agent/turn_budget_checkpoint_test.go`: verify budget checked before each tool
- `internal/agent/partial_batch_return_test.go`: graceful mid-batch stop
- `internal/agent/tool_state_persistence_test.go`: sidecar write/read

### Integration Tests

- `internal/agent/budget_exceeded_during_tool_test.go`: simulate budget expiring mid-bash
- `internal/agent/interrupt_during_batch_test.go`: cancel context during tool batch
- `internal/agent/recovery_completed_results_test.go`: inject results on recovery

### Manual Validation

1. Long session simulation: 1000+ tool calls with artificial budget limits
2. Interrupt scenario: send SIGINT during bash execution, verify graceful cleanup
3. Crash scenario: SIGKILL during tool execution, verify sidecar enables recovery

## Rollout Plan

### PR 1: Tool-Level Budget Checkpoints (Current Branch)

Files to modify:
- `internal/agent/run_budget.go`: add `nearLimit()` method
- `internal/agent/tool_executor.go`: add `checkToolBoundaryBudget()`
- `internal/agent/interrupted_recovery.go`: record not-started tools
- Tests: unit tests for budget threshold logic

Estimated size: +300 lines, 4 files changed

### PR 2: Interrupt Signal Handling (Follow-up)

Files to modify:
- `internal/agent/tool_executor.go`: add deferred state persistence
- `internal/agent/recovery_sidecar.go` (new): sidecar write/read
- `internal/tool/builtin/bash.go`: progress streaming
- Tests: interrupt simulation tests

Estimated size: +400 lines, 5 files changed

### PR 3: Completed Result Injection (Follow-up)

Files to modify:
- `internal/agent/interrupted_recovery.go`: inject results on recovery
- `internal/agent/session.go`: InsertBeforeRecovery method
- Desktop bridge: expose completed results in UI
- Tests: recovery result injection tests

Estimated size: +250 lines, 4 files changed

## Open Questions (Requiring User Input)

1. **Default tool cost estimates**: Should bash default to 10s? Should we learn from
   historical execution times per tool/command pattern?

2. **Budget threshold margin**: How close to the limit before stopping? 10% remaining?
   Fixed buffer (30s wall, 10k tokens)?

3. **Partial batch UX**: When mid-batch stop happens, should Desktop show "partial
   completion" badge or treat it like a normal turn end?

4. **Sidecar cleanup**: How long to retain recovery sidecars? Delete after successful
   recovery? Keep for 24h for crash debugging?

## Success Criteria

1. Issue #9825 scenarios no longer lose completed work
2. Long sessions (1000+ tools) complete without user-visible interruptions
3. Budget-exceeded turns return partial results gracefully
4. External interrupts (SIGINT, timeout) leave recoverable state
5. No regression in normal (non-interrupted) turn latency

## Documentation Updates

- `docs/TURN_BUDGET.md`: explain tool-level checkpoints and estimates
- `docs/INTERRUPTED_TURN_RECOVERY_DESIGN.md`: implementation completed checklist
- `docs/CLI.md`: document recovery sidecar location and manual inspection

## Cache Impact

**Cache-impact: low** - budget check logic is runtime-only, never enters system prompt

**Cache-guard**: existing `internal/agent/interrupted_recovery_test.go` asserts
recovery block format; new tests assert checkpoint logic without changing wire format

**System-prompt-review**: not required - no prompt text changes in this phase
