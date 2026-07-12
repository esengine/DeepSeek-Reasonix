package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/autoresearch"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

// turnOrchestrator owns foreground turn execution while Controller keeps the
// public ports, run-state guard, and session-scoped dependencies.
type turnOrchestrator struct {
	c *Controller
}

type orchestratedTurn struct {
	input          string
	raw            string
	display        string
	editedOriginal string
	synthetic      bool
}

func newTurnOrchestrator(c *Controller) *turnOrchestrator {
	return &turnOrchestrator{c: c}
}

func (o *turnOrchestrator) runTurnWithRawDisplay(ctx context.Context, input, raw, display string) error {
	return o.runOrchestratedTurn(ctx, orchestratedTurn{input: input, raw: raw, display: display})
}

func (o *turnOrchestrator) runEditedTurnWithRawDisplay(ctx context.Context, input, raw, display, original string) error {
	return o.runOrchestratedTurn(ctx, orchestratedTurn{input: input, raw: raw, display: display, editedOriginal: original})
}

func (o *turnOrchestrator) runSyntheticTurnWithRawDisplay(ctx context.Context, input, raw, display string) error {
	return o.runOrchestratedTurn(ctx, orchestratedTurn{input: input, raw: raw, display: display, synthetic: true})
}

func (o *turnOrchestrator) runComposedSyntheticTurn(ctx context.Context, text string) error {
	c := o.c
	return c.runner.Run(agent.WithMemoryCompilerSkip(ctx), c.ComposeSynthetic(text))
}

// runSubagentSkillGoalLoop executes a slash-invoked runAs=subagent skill as a
// real isolated child turn, then lets an active goal continue just as an inline
// skill turn did before.
func (o *turnOrchestrator) runSubagentSkillGoalLoop(ctx context.Context, sk skill.Skill, task, raw, display string, runner skill.SubagentRunner, planMode bool) error {
	return o.runSubagentSkillTurnsGoalLoop(ctx, []skill.Skill{sk}, task, raw, display, runner, planMode)
}

func (o *turnOrchestrator) runSubagentSkillTurnsGoalLoop(ctx context.Context, skills []skill.Skill, task, raw, display string, runner skill.SubagentRunner, planMode bool) error {
	if err := o.runSubagentSkillTurns(ctx, skills, task, raw, display, runner, planMode); err != nil {
		if ctx.Err() != nil {
			o.c.stopGoal(GoalStatusStopped)
		}
		return err
	}
	return o.continueGoal(ctx)
}

// runSubagentSkillTurns records the composed user task and distilled child
// answers only. Child reasoning and tool chatter stay out of the
// provider-visible parent context while their UI events nest under synthetic
// top-level run_skill cards.
func (o *turnOrchestrator) runSubagentSkillTurns(ctx context.Context, skills []skill.Skill, task, raw, display string, runner skill.SubagentRunner, planMode bool) error {
	c := o.c
	c.maybeSessionStart(ctx)
	parentSession := c.parentSessionID()
	images := c.inputImages(raw)
	ctx = agent.WithParentSession(ctx, parentSession)
	ctx = jobs.WithSession(ctx, parentSession)
	ctx = agent.WithUserImages(ctx, images)
	ctx = agent.WithResponseLanguagePreference(ctx, c.responseLanguage)
	ctx = agent.WithReasoningLanguagePreference(ctx, c.reasoningLanguage)

	input := c.compose(task, raw, true)
	startMessages := c.messageCount()
	defer c.snapshotActivityIfChanged(startMessages)
	defer c.recordDisplayForNewUser(startMessages, display)
	c.beginCheckpoint(input)
	if c.guardianSess != nil {
		c.guardianSess.ResetTurn()
	}
	if c.hooks.Enabled() {
		c.mu.Lock()
		c.turn++
		turn := c.turn
		c.mu.Unlock()
		if block, _ := c.hooks.PromptSubmit(ctx, input, turn); block {
			return nil
		}
		defer func() { c.hooks.Stop(context.Background(), lastAssistantText(c.History()), turn) }()
	}

	c.markInFlightTurn(startMessages, true)
	inFlight := true
	defer func() {
		if inFlight {
			c.clearInFlightTurn()
		}
	}()
	c.sink.Emit(event.Event{Kind: event.TurnStarted})
	if c.executor == nil {
		return fmt.Errorf("subagent slash invocation requires an active session")
	}
	c.executor.Session().Add(provider.Message{Role: provider.RoleUser, Content: input, Images: images})

	for _, sk := range skills {
		callID := fmt.Sprintf("slash-skill-%d", c.slashSkillSeq.Add(1))
		args, _ := json.Marshal(map[string]string{"name": sk.Name, "arguments": task})
		toolEvent := event.Tool{
			ID:       callID,
			Name:     "run_skill",
			Args:     string(args),
			ReadOnly: planMode || sk.ReadOnly,
		}
		if c.skillProfile != nil {
			toolEvent.Profile = c.skillProfile(sk)
		}
		c.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: toolEvent})
		runCtx := agent.WithToolCallContext(ctx, callID, c.sink, c, planMode)
		runCtx = agent.WithSubagentDepth(runCtx, 0)
		answer, err := runner(runCtx, sk, input, skill.SubagentRunOptions{HostInitiated: true})
		if err != nil {
			toolEvent.Err = err.Error()
			c.sink.Emit(event.Event{Kind: event.ToolResult, Tool: toolEvent})
			return err
		}
		answer = tool.GuardSubagentHostDecisionText(answer)
		toolEvent.Output = answer
		c.sink.Emit(event.Event{Kind: event.ToolResult, Tool: toolEvent})
		c.executor.Session().Add(provider.Message{Role: provider.RoleAssistant, Content: answer})
		c.sink.Emit(event.Event{Kind: event.Text, Text: answer})
		c.sink.Emit(event.Event{Kind: event.Message, Text: answer})
	}

	c.clearInFlightTurn()
	inFlight = false
	return nil
}

func (o *turnOrchestrator) runOrchestratedTurn(ctx context.Context, turn orchestratedTurn) error {
	c := o.c
	c.maybeSessionStart(ctx)
	if !turn.synthetic {
		c.maybeAutoPlan(ctx, turn.raw)
	}
	parentSession := c.parentSessionID()
	ctx = agent.WithParentSession(ctx, parentSession)
	ctx = jobs.WithSession(ctx, parentSession)
	ctx = agent.WithUserImages(ctx, c.inputImages(turn.input))
	// Synthetic, controller-injected turns (goal-loop continuation,
	// plan-approved execution, …) must not be Memory v5-compiled: compiling them
	// re-injects a contract the model echoes back, which spins the goal loop
	// forever (#5342, #5329). Only genuine user turns supply a compiler source.
	if turn.synthetic || IsSyntheticUserMessage(turn.raw) {
		ctx = agent.WithMemoryCompilerSkip(ctx)
	} else {
		ctx = agent.WithMemoryCompilerSourceInput(ctx, turn.raw)
	}
	input := c.compose(turn.input, turn.raw, !turn.synthetic)
	startMessages := c.messageCount()
	defer c.snapshotActivityIfChanged(startMessages)
	defer c.recordDisplayForNewUser(startMessages, turn.display)
	if turn.editedOriginal != "" {
		defer c.markEditedForNewUser(startMessages, turn.editedOriginal)
	}
	// Open a checkpoint only for visible user turns before the user message is
	// appended, so the recorded message boundary precedes it and pre-edit
	// snapshots land here. Synthetic continuations stay attached to the visible
	// turn that spawned them; otherwise hidden user-role messages would advance
	// backend checkpoint turns without a matching frontend turn.
	if !turn.synthetic {
		c.beginCheckpoint(input)
	}
	if c.guardianSess != nil {
		c.guardianSess.ResetTurn()
	}
	// UserPromptSubmit / Stop hooks bracket the whole turn (incl. the plan
	// research + approved-execution sub-turns below): a gating UserPromptSubmit
	// aborts before any model call; Stop fires once when the turn returns.
	if c.hooks.Enabled() {
		c.mu.Lock()
		c.turn++
		turn := c.turn
		c.mu.Unlock()
		if block, _ := c.hooks.PromptSubmit(ctx, input, turn); block {
			return nil // the hook's notify callback already surfaced the reason
		}
		defer func() { c.hooks.Stop(context.Background(), lastAssistantText(c.History()), turn) }()
	}
	c.markInFlightTurn(startMessages, !turn.synthetic && !IsSyntheticUserMessage(turn.raw))
	autoResearchTaskID := c.goals.currentAutoResearchTaskID()
	autoResearchAcceptedBefore := c.autoResearchAcceptedEvidenceIDs(autoResearchTaskID)
	c.appendAutoResearchHeartbeat(autoResearchTaskID, autoresearch.HeartbeatStartingTurn, "")
	modelInput := input
	if !turn.synthetic {
		modelInput = c.withCapabilityRoute(input, turn.raw)
	}
	err := c.runner.Run(ctx, modelInput)
	if err == nil {
		c.recordAutoResearchEvidenceFromAssistant(autoResearchTaskID, lastAssistantText(c.History()))
		c.recordAutoResearchTurnProgress(autoResearchTaskID, autoResearchAcceptedBefore)
		c.appendAutoResearchHeartbeat(autoResearchTaskID, autoresearch.HeartbeatTurnDone, "")
		c.clearInFlightTurn()
	} else {
		c.appendAutoResearchHeartbeat(autoResearchTaskID, autoresearch.HeartbeatWarning, err.Error())
		// When the user explicitly cancels (Ctrl+C), the incomplete turn's
		// assistant messages and tool results are already saved to the
		// session. If they stay, the next turn's model sees leftover
		// in-progress todo items and partial tool calls and may re-execute
		// the interrupted work. Keep the real user prompt for visible turns so
		// follow-up questions and resumes do not lose the user's context (#5499).
		if errors.Is(err, context.Canceled) && c.CancelRequested() {
			if turn.synthetic || IsSyntheticUserMessage(turn.raw) {
				c.stripTurnMessagesAfter(startMessages)
			} else {
				c.stripCancelledVisibleTurnMessagesAfter(startMessages)
			}
		}
		c.clearInFlightTurn()
		return err
	}
	c.mu.Lock()
	plan := c.planMode
	c.mu.Unlock()
	if !plan {
		return nil
	}
	proposal := lastAssistantText(c.History())
	if proposal == "" {
		return nil // no substantive proposal to gate
	}
	// The plan is already visible as the assistant's answer, so the request
	// carries no subject — it's purely the gate.
	allow, _, err := c.requestApproval(ctx, planApprovalTool, "", nil)
	if err != nil {
		return err
	}
	if !allow {
		return nil // keep planning; plan mode stays on
	}
	c.SetPlanMode(false)
	todoArgs := c.seedPlanTodos(proposal)
	execStart := c.sessionMessageCount()
	// The plan is the go-ahead: don't re-prompt for each write of the approved
	// work. Auto-approve writers for the duration of this execution turn only; a
	// later turn (even "continue") falls back to the normal per-tool approval.
	c.approval.setPlanAutoApprove(true)
	defer c.approval.setPlanAutoApprove(false)
	err = func() error {
		c.markInFlightTurn(execStart, false)
		defer c.clearInFlightTurn()
		return o.runComposedSyntheticTurn(ctx, planApprovedMessage)
	}()
	if err != nil {
		if errors.Is(err, context.Canceled) && c.CancelRequested() {
			c.stripTurnMessagesAfter(execStart)
		}
		return err
	}
	if todoArgs != "" && !c.hasTodoUpdateSince(execStart) {
		c.completePlanTodos(todoArgs)
	}
	return nil
}

func (o *turnOrchestrator) runGoalLoopWithRawDisplay(ctx context.Context, input, raw, display string) error {
	if err := o.runTurnWithRawDisplay(ctx, input, raw, display); err != nil {
		if ctx.Err() != nil {
			o.c.stopGoal(GoalStatusStopped)
		}
		return err
	}
	return o.continueGoal(ctx)
}

func (o *turnOrchestrator) runEditedGoalLoopWithRawDisplay(ctx context.Context, input, raw, display, original string) error {
	if err := o.runEditedTurnWithRawDisplay(ctx, input, raw, display, original); err != nil {
		if ctx.Err() != nil {
			o.c.stopGoal(GoalStatusStopped)
		}
		return err
	}
	return o.continueGoal(ctx)
}

func (o *turnOrchestrator) continueGoal(ctx context.Context) error {
	c := o.c
	for {
		cont := o.advanceGoalAfterTurn()
		if !cont {
			return nil
		}
		if err := ctx.Err(); err != nil {
			c.stopGoal(GoalStatusStopped)
			return err
		}
		turn := goalContinueTurn
		if msg, ok := c.goals.takeIntercept(); ok {
			turn = msg
			if strings.Contains(msg, "AutoResearch readiness check failed") {
				c.noticeDetail("Goal is not ready to complete yet; continuing the remaining work.", msg)
			} else {
				c.noticeDetail("Goal still has unfinished task state; continuing the remaining work.", msg)
			}
		}
		if err := o.runSyntheticTurnWithRawDisplay(ctx, turn, turn, ""); err != nil {
			if ctx.Err() != nil {
				c.stopGoal(GoalStatusStopped)
			}
			return err
		}
	}
}

func (o *turnOrchestrator) advanceGoalAfterTurn() bool {
	c := o.c
	// Gather every input the FSM needs off the goal lock: parse the marker,
	// snapshot the executor's todos + readiness, and check tool activity. None
	// of these touch goal state, so the machine's critical section stays pure.
	status, reason, _ := parseGoalStatusMarker(lastAssistantText(c.History()))
	autoResearchTaskID := c.goals.currentAutoResearchTaskID()
	var readiness string
	if c.executor != nil {
		readiness = c.executor.GoalReadinessFailure()
	}
	if arReadiness := c.autoResearchReadinessFailure(); arReadiness != "" {
		if readiness != "" {
			readiness += "\n" + arReadiness
		} else {
			readiness = arReadiness
		}
	}
	res := c.goals.advance(goalAdvanceInput{
		status:     status,
		reason:     reason,
		toolCalled: c.toolWasCalledLastTurn(),
		todos:      c.goalTodos(),
		readiness:  readiness,
	})
	c.persistGoalState(res.path, res.data, res.ok)
	if res.notice != "" {
		c.finalizeAutoResearchTask(autoResearchTaskID, res.notice)
		c.notice(res.notice)
	}
	if res.notice == goalCompleteNotice && c.executor != nil {
		c.completeRemainingGoalTodos()
	}
	return res.cont
}

func (c *Controller) finalizeAutoResearchTask(taskID, notice string) {
	if c.autoResearch == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	switch {
	case notice == goalCompleteNotice:
		status := autoresearch.StatusComplete
		if _, err := c.autoResearch.UpdateProgress(taskID, autoresearch.ProgressPatch{Status: &status}); err != nil {
			c.noticeDetail("AutoResearch status update failed.", "autoresearch task completion update failed: "+err.Error())
			return
		}
		c.notice("autoresearch task completed: " + taskID)
	case strings.HasPrefix(notice, "goal blocked: ") || notice == "goal continuation limit reached":
		status := autoresearch.StatusBlocked
		reason := strings.TrimPrefix(notice, "goal blocked: ")
		if reason == "" {
			reason = notice
		}
		if _, err := c.autoResearch.UpdateProgress(taskID, autoresearch.ProgressPatch{Status: &status, BlockedReason: &reason}); err != nil {
			c.noticeDetail("AutoResearch status update failed.", "autoresearch task blocked update failed: "+err.Error())
			return
		}
		c.noticeDetail("AutoResearch task marked blocked.", "autoresearch task blocked: "+taskID+"\nreason: "+reason)
	}
}

// completeRemainingGoalTodos force-completes any remaining incomplete canonical
// todos when the goal FSM transitions to completed and emits a synthetic
// todo_write event so the frontend panel reflects the final state. Handles the
// second [goal:complete] override (non-strict) where the model does not mark
// each todo individually.
func (c *Controller) completeRemainingGoalTodos() {
	todos := c.executor.CanonicalTodoState()
	if len(evidence.IncompleteTodos(todos)) == 0 {
		return
	}
	for i := range todos {
		todos[i].Status = "completed"
	}
	args, err := json.Marshal(map[string]any{"todos": todos})
	if err != nil {
		return
	}
	t := event.Tool{ID: "goal-final", Name: "todo_write", Args: string(args), ReadOnly: true}
	c.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: t})
	t.Output = "goal completed"
	c.sink.Emit(event.Event{Kind: event.ToolResult, Tool: t})
	c.executor.ReplaceTodoState(todos)
	// Persist the completed todo state so a session reload does not revert
	// to the old incomplete list — the synthetic todo_write events are not
	// part of the session transcript and rebuildTodoState would otherwise
	// reconstruct the stale pre-completion state.
	c.goals.persistWithTodos(todos)
}
