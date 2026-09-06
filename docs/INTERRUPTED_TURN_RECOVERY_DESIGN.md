# Interrupted Turn Recovery Design

## Context

Issue #9825 reports frequent turn interruptions in long sessions with high tool
density. The host injects `interrupted-turn-recovery` blocks asking the model to
retry interrupted tools, but:

1. Completed tool results are not returned to the model before interruption
2. Recovery hints lack complete arguments, making safe retry impossible
3. Multiple consecutive interruptions degrade UX to cliff-edge failures

The current branch (`fix/interrupted-turn-recovery`) has implemented the core
infrastructure. This doc organizes what exists, what remains, and the delivery plan.

## Implemented (Current Branch)

### Tool Execution State Classification

`internal/provider/tool_recovery.go` defines `ToolRunState` and classification logic:

- `ToolRunCompleted`: verified execution finished successfully
- `ToolRunCancelled`: proven not to have started (context cancelled before dispatch)
- `ToolRunUnknown`: execution may have side effects; unsafe to blindly retry
- `ToolRunNotStarted` / `ToolRunPending`: legacy states, mapped to cancelled or unknown

`RecordToolRecovery` populates `InterruptedTurnRecovery` fields based on state:
- `CompletedTools`: safe to skip on retry
- `CancelledTools` / `NotStartedTools`: safe to retry with full arguments
- `UnknownTools`: requires inspection before retry
- Legacy `InterruptedTools` string list preserved for old readers

### Write Intent Recording and Verification

`internal/agent/write_recovery.go` implements:

- `addWriteIntent`: records file write intents durably in `ToolCall.WriteIntents`
- `verifyInterruptedWrites`: checks postconditions via `tool.WriteVerifier`
- `WriteRecoveryCheck`: per-file postcondition state (satisfied/unknown/absent)
- `recoverPreviousWrite`: blocks duplicate writes if postconditions already satisfied
- `recoverPreviousUnknown`: blocks retries of unknown-outcome side-effecting calls

Write intents are captured at tool execution boundaries via `tool.WithWriteIntentHook`,
persisted in the durable conversation, and verified on next turn startup.

### Recovery Identity and Checkpoints

Recent commits:
- `1f680d85f feat: add recovery identity to checkpoints`
- `a03c4d3ab feat: persist tool execution start barrier`

Recovery identity (`agentID`, `taskID`, `turnID`, `attemptID`) is now persisted in
checkpoint records, allowing Desktop and CLI to distinguish recovery attempts across
crashes, interruptions, and manual retries.

Tool execution start barriers record when a tool dispatch becomes durable, separating
"cancelled before start" from "started but interrupted."

### Desktop Integration

Commits like `b72c6baf9 feat: expose recovery-required turn states to desktop` and
`165a460d3 feat: persist interruption recovery state in lifecycle events` surface
recovery state to Desktop UI, enabling:

- Pending recovery badge in session list
- Recovery-required turn markers in transcript
- Host-bridge recovery status queries

### Provider Transcript Validation

`94a7d9356 feat: validate normalized provider transcripts` ensures that recovery
decisions are based on protocol-correct conversation state, not on corrupted or
malformed history that could lead to unsafe retries.

## Remaining Work

### 1. Graceful Turn Budget Management

**Problem**: Issue #9825 reports that turns are interrupted mid-execution when soft
budget (round/elapsed-time) is exceeded, rather than finishing the current tool and
gracefully ending the turn.

**Solution**: Implement turn budget checkpoints at tool boundaries:

- Before dispatching each tool, check remaining turn budget
- If budget will be exceeded by typical tool execution time, emit a graceful
  end-of-turn signal with partial results
- Return completed tool results to the model, then stop with `stop_reason=length`
  or a new `stop_reason=budget_exceeded`
- Desktop shows "Turn ended early due to budget limit" rather than "Interrupted"

**Implementation sketch**:

```go
// internal/agent/turn_budget.go (new file)

type turnBudgetPolicy struct {
    softLimit    time.Duration  // warn threshold
    hardLimit    time.Duration  // force stop
    toolTimeout  time.Duration  // estimated per-tool time
}

func (a *Agent) checkTurnBudget(ctx context.Context) (gracefulStop bool, err error) {
    elapsed := time.Since(a.turn.startedAt)
    if elapsed >= a.turnBudget.hardLimit {
        return false, fmt.Errorf("turn hard budget exceeded")
    }
    if elapsed+a.turnBudget.toolTimeout >= a.turnBudget.softLimit {
        // Emit partial results and stop gracefully
        return true, nil
    }
    return false, nil
}
```

Call `checkTurnBudget` in `runToolLoop` before each tool dispatch. If graceful stop
is signaled, return completed tool results in an assistant message with a stop reason
indicating budget limit reached.

### 2. Complete Argument Recovery for Large Tools

**Problem**: `interrupted-turn-recovery` blocks contain only tool names and IDs, not
full arguments. For large tools like `edit_file` or `write_file`, the model cannot
safely retry without inspecting file state first.

**Current state**: `InterruptedTurnRecovery.ToolCalls []ToolCallRecord` already
persists full arguments in the durable local-only message, but these are not exposed
to the model in the recovery block due to context size concerns.

**Solution**: Implement argument recovery UI affordances:

- Desktop: "Retry interrupted tool" button in recovery banner, which injects the
  full tool call (with arguments) back into the conversation as a host-approved retry
- CLI: `reasonix recover --turn <id>` command to list interrupted tools with arguments,
  allowing user to select which to retry
- Model-facing block continues to omit arguments, but includes `argument_digest` hash
  for disambiguation

**Implementation**:

```go
// internal/agent/interrupted_recovery.go

func interruptedRecoveryBlock(r *provider.InterruptedTurnRecovery) string {
    // ... existing code ...
    
    // Add argument digests for user-facing retry
    if len(r.ToolCalls) > 0 {
        b.WriteString("argument_recovery_available: use host retry affordance\n")
        for _, call := range r.ToolCalls {
            if call.State == provider.ToolRunUnknown || call.State == provider.ToolRunCancelled {
                hash := sha256.Sum256(call.Arguments)
                fmt.Fprintf(&b, "- id=%s digest=%x\n", call.ID, hash[:8])
            }
        }
    }
}
```

Desktop recovery bridge (new):

```go
// desktop/recovery_retry.go

func (a *App) RetryInterruptedTool(callID string) error {
    recovery := a.tab.agent.PendingInterruptedRecovery()
    var call *provider.ToolCallRecord
    for _, c := range recovery.ToolCalls {
        if c.ID == callID {
            call = &c
            break
        }
    }
    if call == nil {
        return fmt.Errorf("tool call not found: %s", callID)
    }
    
    // Inject as a new assistant message with host approval
    return a.tab.controller.InjectToolRetry(call)
}
```

### 3. Return Completed Tool Results Before Interruption

**Problem**: When a tool completes but the turn is interrupted before the model can
read the result, the result is lost. Recovery hints say "completed_tools: bash" but
the model has no context about what the bash call did.

**Current state**: Completed tool results are stored in `a.turn.toolResults` but
discarded when the turn is interrupted.

**Solution**: Persist completed tool results in the recovery handoff:

- When interruption is detected, serialize completed tool results into a durable
  local-only message
- On recovery, inject these results into the conversation as fenced tool-result
  messages before the recovery block
- Mark them `LocalOnly: true` so they don't go to the provider, but surface them in
  Desktop UI and include them in the model's recovery context

**Implementation**:

```go
// internal/agent/interrupted_recovery.go

func (a *Agent) persistCompletedToolResults(r *provider.InterruptedTurnRecovery) error {
    var results []provider.Message
    for _, summary := range r.CompletedTools {
        result, ok := a.turn.toolResults[summary.ID]
        if !ok {
            continue
        }
        results = append(results, provider.Message{
            Role:      provider.RoleTool,
            ToolCallID: summary.ID,
            Content:   result.output,
            LocalOnly: true,
            RecoveryEvidence: true,
        })
    }
    
    if len(results) > 0 {
        return a.sess.conversation.AppendMany(results)
    }
    return nil
}
```

Call this from the interruption detection path before persisting the recovery marker.

### 4. Atomic Write Operations

**Problem**: Some write tools may leave partial state if interrupted mid-execution.

**Current state**: `tool.WriteVerifier` checks postconditions, but does not prevent
partial writes during execution.

**Solution**: Implement atomic write pattern for file tools:

- Write to temporary file first (`.reasonix-tmp-<uuid>`)
- On completion, atomically rename to target path
- On interruption, temp file is left behind; postcondition check detects "absent"
- Recovery verification sees incomplete write and allows safe retry

**Implementation**: This is tool-specific. For `edit_file` and `write_file`:

```go
// internal/tool/builtin/file_edit.go

func (t *EditFile) Execute(ctx context.Context, args EditFileArgs) (string, error) {
    tmpPath := args.Path + ".reasonix-tmp-" + uuid.New().String()
    defer os.Remove(tmpPath)
    
    // Write to temp file
    if err := os.WriteFile(tmpPath, newContent, 0644); err != nil {
        return "", err
    }
    
    // Atomic rename
    if err := os.Rename(tmpPath, args.Path); err != nil {
        return "", err
    }
    
    return "File edited successfully", nil
}
```

Postcondition check verifies final file content matches expected hash.

## Testing Strategy

### Unit Tests

- `internal/agent/write_recovery_test.go`: verify postcondition logic
- `internal/provider/tool_recovery_state_test.go`: state classification edge cases
- `internal/agent/interrupted_recovery_test.go`: recovery block formatting

### Integration Tests

- `internal/agent/loop_e2e_test.go`: simulate interruption during tool execution
- Desktop lifecycle tests: recovery state transitions across restart
- Cross-provider tests: recovery works with all three adapters (Chat/Messages/Responses)

### Manual Validation

- Long session simulation: run 1000+ tool calls, inject artificial interruptions
- Budget exhaustion scenario: set soft budget to 30s, verify graceful stop
- Large write scenario: interrupt `write_file` with 10MB content, verify atomicity

## Documentation Impact

New user-facing docs:

- `docs/TURN_INTERRUPTION_RECOVERY.md`: explain recovery UX to users
- `docs/TURN_BUDGET.md`: document turn budget policy and graceful limits
- Update `docs/ACP.md`: recovery state in ACP protocol

Update existing:

- `docs/CLI.md`: document `reasonix recover` command
- `docs/EXTENSIONS.md`: recovery lifecycle hooks for extensions

## Cache Impact

**Cache-impact: medium** - recovery block format changes affect cache prefix

Existing recovery block:
```
<interrupted-turn-recovery>
completed_tools: bash
interrupted_tools: edit_file
</interrupted-turn-recovery>
```

New recovery block adds fields:
```
<interrupted-turn-recovery>
cause: turn budget exceeded
completed_tools: bash files=src/main.go diff=+5/-2
interrupted_tools: none
outcome_unknown_tools:
- edit_file id=call_abc123
write_postcondition: id=call_abc123 path=src/main.go state=satisfied
</interrupted-turn-recovery>
```

**Cache-guard**: `internal/agent/interrupted_recovery_test.go` asserts exact block
format. Any format change requires updating golden fixtures.

**System-prompt-review**: @SivanCola to review recovery instruction text changes.

## Delivery Plan

### Phase 1: Infrastructure Complete (✓ Done in Current Branch)

- [x] Tool execution state classification
- [x] Write intent recording
- [x] Recovery identity in checkpoints
- [x] Desktop recovery state exposure
- [x] Provider transcript validation

### Phase 2: Graceful Budget Management (Next PR)

- [ ] Implement turn budget checkpoints
- [ ] Graceful stop before hard limit
- [ ] Return partial results on budget exhaustion
- [ ] Desktop "budget exceeded" UI distinct from "interrupted"

### Phase 3: Argument Recovery Affordances (Subsequent PR)

- [ ] Desktop retry button with full arguments
- [ ] CLI `reasonix recover` command
- [ ] Argument digest in recovery block

### Phase 4: Completed Results Persistence (Subsequent PR)

- [ ] Persist completed tool results in recovery handoff
- [ ] Inject as fenced local-only messages on recovery
- [ ] Desktop displays completed results in recovery banner

### Phase 5: Atomic Writes (Incremental per Tool)

- [ ] Atomic write pattern for `edit_file`
- [ ] Atomic write pattern for `write_file`
- [ ] Postcondition verification for atomic writes

## Open Questions

1. **Turn budget defaults**: What are reasonable soft/hard limits? 2min/5min? Should
   these be configurable per-session or per-model?

2. **Completed result size limits**: If a bash call produces 100KB of output before
   interruption, do we persist all of it in the recovery handoff? Or clip to N KB?

3. **Recovery retry budget**: If the model retries an interrupted tool and it fails
   again, how many retry attempts do we allow before forcing user intervention?

4. **Cross-session recovery**: Should recovery state survive session restart? Or is
   it scoped to the current process lifetime?

## References

- Issue #9825: Primary bug report with user impact details
- `internal/provider/tool_recovery.go`: State classification logic
- `internal/agent/write_recovery.go`: Write intent verification
- `internal/agent/interrupted_recovery.go`: Recovery block formatting
- `docs/OPENCODE_RECOVERY_COMPARISON.md`: OpenCode analysis and recommendations
