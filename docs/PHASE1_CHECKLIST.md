# Turn Interruption Recovery: Phase 1 Implementation Checklist

Based on `TURN_INTERRUPTION_IMPLEMENTATION.md`, this tracks Phase 1 progress.

## Phase 1: Tool-Level Budget Checkpoints

**Goal**: Check budget before each tool dispatch to prevent mid-tool interruptions.

### Core Implementation

- [ ] `internal/agent/run_budget.go`: Add `nearLimit()` method
  - [ ] Token projection with estimate buffer
  - [ ] Wall time projection with estimate buffer
  - [ ] Cost projection (optional, requires pricing lookup)
  
- [ ] `internal/agent/run_budget.go`: Add tool cost estimates
  - [ ] `toolCostEstimate` struct (estimatedSeconds, estimatedTokens)
  - [ ] Default estimates map (bash: 10s, edit_file: 2s, etc.)
  - [ ] Lookup helper for tool name → estimate
  
- [ ] `internal/agent/tool_executor.go`: Budget checkpoint in tool loop
  - [ ] `checkToolBoundaryBudget()` - called before each tool
  - [ ] Extract tool estimate based on call name
  - [ ] Call `runBudget.nearLimit()` with estimate
  
- [ ] `internal/agent/tool_executor.go`: Partial batch handling
  - [ ] `returnPartialBatchResults()` - append completed results
  - [ ] `recordNotStartedTools()` - persist remaining calls
  - [ ] `gracefulBudgetStop` error type
  
- [ ] `internal/agent/interrupted_recovery.go`: Not-started tool recording
  - [ ] Extend `InterruptedTurnRecovery` with not-started metadata
  - [ ] Format not-started tools in recovery block
  - [ ] Preserve full arguments for safe retry

### Testing

- [ ] `internal/agent/near_limit_test.go`
  - [ ] Token threshold with estimate buffer
  - [ ] Wall time threshold with estimate buffer
  - [ ] Boundary cases (exactly at limit, 1 token under, etc.)
  
- [ ] `internal/agent/tool_boundary_checkpoint_test.go`
  - [ ] Budget checked before each tool in batch
  - [ ] First tool completes, second hits threshold → partial return
  - [ ] All tools fit within budget → normal execution
  
- [ ] `internal/agent/partial_batch_return_test.go`
  - [ ] Completed results appended to conversation
  - [ ] Remaining tools marked not-started in recovery
  - [ ] Turn ends with taskBudgetPause
  - [ ] Next turn can retry not-started tools with full arguments

### Integration

- [ ] `internal/agent/run_loop.go`: Wire checkpoint into executeToolBatch
  - [ ] Call checkToolBoundaryBudget before each executeOne
  - [ ] Handle gracefulBudgetStop return
  - [ ] Emit partial results before ending turn
  
- [ ] `internal/agent/finalization.go`: Partial batch pause message
  - [ ] Distinct notice for "stopped before tool X" vs "tool X interrupted"
  - [ ] Include completed tool count in detail

### Desktop Integration

- [ ] `desktop/app.go`: Expose partial completion state
  - [ ] `PartialBatchCompletion` field in turn metadata
  - [ ] Frontend shows "N/M tools completed" badge
  
- [ ] Desktop UI: Retry not-started tools button
  - [ ] Read not-started tools from recovery state
  - [ ] Inject retry with full arguments on click

### Documentation

- [ ] Update `docs/TURN_INTERRUPTION_IMPLEMENTATION.md`
  - [ ] Mark Phase 1 tasks completed
  - [ ] Document default estimate values chosen
  - [ ] Add example of budget checkpoint preventing interruption
  
- [ ] Create `docs/TURN_BUDGET.md`
  - [ ] Explain three budget axes (tokens, cost, wall)
  - [ ] Document tool-level checkpoint behavior
  - [ ] Show example of partial batch graceful stop

## Acceptance Criteria

- [ ] Manual test: Set 60s wall budget, run 5x bash calls (each 15s)
  - Expected: 4 complete, 5th marked not-started, turn ends gracefully
  - Actual: (to be filled after implementation)
  
- [ ] Manual test: Set 50k token budget, run large read_file chain
  - Expected: stops before read that would exceed budget
  - Actual: (to be filled)
  
- [ ] Regression: Normal turns (no budget pressure) show no latency increase
  - Measure: checkpoint overhead < 1ms per tool
  
- [ ] Issue #9825 scenario: Long session with budget pressure
  - Before: frequent "interrupted_tools: bash" with lost results
  - After: graceful stops with completed results preserved

## Blocked / Open Questions

- [ ] **Tool estimate accuracy**: Should we measure actual execution times and
      adjust estimates dynamically? Or keep conservative static values?
      
- [ ] **Threshold margin**: Use 10% remaining budget, or fixed buffer (30s, 10k tokens)?
      Current implementation uses fixed buffer; document the choice.
      
- [ ] **MCP tool estimates**: Generic 5s default vs per-server configuration?
      Should MCP servers declare their own estimates?

## Review Checklist (Before PR)

- [ ] All unit tests pass
- [ ] Integration tests cover happy path and edge cases
- [ ] Manual validation scenarios documented with results
- [ ] Lint passes (run `make lint`)
- [ ] No new struct-state ratchet violations (`go run ./tools/repolint`)
- [ ] PR body includes cache-impact and documentation-impact statements
- [ ] Related issue #9825 linked in PR description

## Estimated Effort

- Core implementation: 4-6 hours
- Testing: 3-4 hours
- Desktop integration: 2-3 hours
- Documentation: 1-2 hours

**Total: 10-15 hours** (roughly 2 work days)

## Next Steps After Phase 1

Once Phase 1 is complete and merged:

1. Evaluate impact on #9825 scenarios - do tool-level checkpoints prevent
   most interruptions, or do we still see mid-tool signal interrupts?
   
2. If still seeing interruptions → prioritize Phase 2 (signal handling)
   
3. If checkpoints solve most cases → Phase 3 (result injection) can be
   lower priority cleanup

4. Gather telemetry: how often does checkpoint trigger? What % of stops
   are graceful vs interrupted?
