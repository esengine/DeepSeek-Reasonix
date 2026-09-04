package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/jobs"
	"reasonix/internal/planmode"
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
	input            string
	raw              string
	imageRefs        string
	images           *turnImages
	display          string
	editedOriginal   string
	synthetic        bool
	goalContinuation *goalContinuationSnapshot
}

func newTurnOrchestrator(c *Controller) *turnOrchestrator {
	return &turnOrchestrator{c: c}
}

func (o *turnOrchestrator) runGoalContinuationTurnWithRawDisplay(
	ctx context.Context,
	input, raw, display string,
	res goalAdvanceResult,
) (bool, error) {
	snapshot, ok := o.c.goals.admitContinuation(res)
	if !ok {
		return false, nil
	}
	err := o.runOrchestratedTurn(ctx, orchestratedTurn{
		input:            input,
		raw:              raw,
		display:          display,
		synthetic:        true,
		goalContinuation: &snapshot,
	})
	return true, err
}

func (o *turnOrchestrator) runComposedSyntheticTurn(ctx context.Context, text string) error {
	c := o.c
	ctx = agent.WithRawUserInput(ctx, text)
	ctx = c.withPlannerTurnMetadata(ctx, text, true)
	return c.runner.Run(ctx, c.ComposeSynthetic(text))
}

// runSubagentSkillGoalLoop executes a slash-invoked runAs=subagent skill as a
// real isolated child turn, then lets an active goal continue just as an inline
// skill turn did before.
func (o *turnOrchestrator) runSubagentSkillGoalLoop(ctx context.Context, sk skill.Skill, task, raw, display string, runner skill.SubagentRunner, planMode bool) error {
	return o.runSubagentSkillTurnsGoalLoop(ctx, []skill.Skill{sk}, task, raw, display, runner, planMode)
}

func (o *turnOrchestrator) runSubagentSkillTurnsGoalLoop(ctx context.Context, skills []skill.Skill, task, raw, display string, runner skill.SubagentRunner, planMode bool) error {
	expectedContinuationEpoch := o.c.goals.continuationToken()
	images := o.c.resolveTurnImages(raw)
	ctx = o.c.withVisionRouting(agent.WithSubagentImageCandidates(ctx, images.candidates))
	// The skill turn's model requests count against the active goal's token
	// budget, so bind a recorder for the span even though the sub-agent cannot
	// call update_goal itself.
	if scopeID, goal, ok := o.c.goals.deliveryScope(); ok {
		ctx = agent.WithDeliveryExecutionScope(ctx, agent.DeliveryExecutionScope{ID: scopeID, TaskText: goal})
		recorder := o.c.goals.newTurnRecorder(scopeID, o.c.goals.continuationToken())
		o.c.goalUsageTee.setActiveRecorder(recorder)
	}
	startMessages := o.c.sessionMessageCount()
	err := o.runSubagentSkillTurns(ctx, skills, task, raw, display, runner, planMode, images)
	o.c.captureGoalRunWorkDuration(startMessages)
	if err != nil {
		if ctx.Err() != nil {
			o.c.goalUsageTee.setActiveRecorder(nil)
			o.c.stopGoal(GoalStatusStopped)
		}
		if !goalTurnErrorAbsorbable(err) || !o.c.goals.active() {
			o.c.goalUsageTee.setActiveRecorder(nil)
			return err
		}
		return o.continueGoal(ctx, expectedContinuationEpoch, err)
	}
	return o.continueGoal(ctx, expectedContinuationEpoch, nil)
}

// runSubagentSkillTurns records the composed user task and distilled child
// answers only. Child reasoning and tool chatter stay out of the
// provider-visible parent context while their UI events nest under synthetic
// top-level run_skill cards.
func (o *turnOrchestrator) runSubagentSkillTurns(ctx context.Context, skills []skill.Skill, task, raw, display string, runner skill.SubagentRunner, planMode bool, images *turnImages) (err error) {
	c := o.c
	turnStartedAt := time.Now()
	c.maybeSessionStart(ctx)
	parentSession := c.parentSessionID()
	ctx = agent.WithParentSession(ctx, parentSession)
	ctx = jobs.WithSession(ctx, parentSession)
	ctx = agent.WithUserImages(ctx, images.userImages)
	ctx = o.c.withVisionRouting(agent.WithSubagentImageCandidates(ctx, images.candidates))
	ctx = agent.WithResponseLanguagePreference(ctx, c.display.responseLanguage)
	ctx = agent.WithReasoningLanguagePreference(ctx, c.display.reasoningLanguage)

	input := c.imageRoutingPrefix(images) + c.compose(task, raw, true)
	startMessages := c.messageCount()
	var marker agent.InFlightTurnMeta
	defer func() { c.finishInFlightTurn(startMessages, marker) }()
	defer c.recordDisplayForNewUser(startMessages, display)
	// The checkpoint prompt labels the turn in the rewind picker (and is
	// prefilled into the composer after a conversation rewind), so it must be
	// the user's own text — never the composed provider input with its
	// transient <response-language>/<reasoning-language>/memory/hook blocks.
	c.beginCheckpoint(ctx, firstNonEmpty(raw, task))
	if c.hooks.Enabled() {
		c.mu.Lock()
		c.turn++
		turn := c.turn
		c.mu.Unlock()
		if block, _ := c.hooks.PromptSubmit(ctx, input, turn); block {
			return nil
		}
		defer func() { c.hooks.StopResult(context.Background(), lastAssistantText(c.History()), turn, err) }()
	}

	ctx, marker = c.beginTurn(ctx, startMessages, true)
	c.sink.Emit(event.Event{Kind: event.TurnStarted})
	if c.executor == nil {
		return fmt.Errorf("subagent slash invocation requires an active session")
	}
	c.executor.Session().Add(provider.Message{Role: provider.RoleUser, Content: input, Images: images.userImages, CreatedAt: time.Now().UnixMilli()})

	for _, sk := range skills {
		sk = c.skills.prepare(sk)
		callID := fmt.Sprintf("slash-skill-%d", c.skills.slashSeq.Add(1))
		args, _ := json.Marshal(map[string]string{"name": sk.Name, "arguments": task})
		toolEvent := event.Tool{
			ID:       callID,
			Name:     "run_skill",
			Args:     string(args),
			ReadOnly: sk.ReadOnly,
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
		workDurationMs := max(int64(1), time.Since(turnStartedAt).Milliseconds())
		c.executor.Session().Add(provider.Message{Role: provider.RoleAssistant, Content: answer, WorkDurationMs: workDurationMs})
		display := agent.DisplayAssistantText(answer)
		c.sink.Emit(event.Event{Kind: event.Text, Text: display})
		c.sink.Emit(event.Event{Kind: event.Message, Text: display})
	}

	return nil
}

func (o *turnOrchestrator) runOrchestratedTurn(ctx context.Context, turn orchestratedTurn) (err error) {
	c := o.c
	c.maybeSessionStart(ctx)
	parentSession := c.parentSessionID()
	ctx = agent.WithParentSession(ctx, parentSession)
	ctx = jobs.WithSession(ctx, parentSession)
	ctx, images := c.bindOrchestratedTurnImages(ctx, turn)
	ctx = agent.WithRawUserInput(ctx, turn.raw)
	continuation := turn.goalContinuation
	var input string
	if continuation != nil {
		input = c.composeWithGoal(
			turn.input,
			turn.raw,
			false,
			continuation.goal,
			GoalStatusRunning,
		)
	} else {
		input = c.compose(turn.input, turn.raw, !turn.synthetic)
	}
	input = c.imageRoutingPrefix(images) + input
	// input.receive: the composed text crosses the extension chain before it
	// enters the session (checkpoint, hooks, and the model all see the final
	// text). A block ruling aborts the turn with the redacted reason surfaced,
	// mirroring the PromptSubmit hook's abort path; a required-class extension
	// failure fails the turn.
	input, blocked, interceptErr := c.interceptInputReceive(ctx, input)
	if interceptErr != nil {
		return interceptErr
	}
	if blocked {
		return nil
	}
	startMessages := c.messageCount()
	var marker agent.InFlightTurnMeta
	defer func() { c.finishInFlightTurn(startMessages, marker) }()
	defer c.recordDisplayForNewUser(startMessages, turn.display)
	if turn.editedOriginal != "" {
		defer c.markEditedForNewUser(startMessages, turn.editedOriginal)
	}
	// Open a checkpoint only for visible user turns before the user message is
	// appended, so the recorded message boundary precedes it and pre-edit
	// snapshots land here. Synthetic continuations stay attached to the visible
	// turn that spawned them; otherwise hidden user-role messages would advance
	// backend checkpoint turns without a matching frontend turn. The label is
	// the user's own text (raw, falling back to the expanded input) — the
	// composed provider input carries transient prefab blocks that must never
	// surface in the rewind picker or be prefilled into the composer.
	if !turn.synthetic {
		c.beginCheckpoint(ctx, firstNonEmpty(turn.raw, turn.input))
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
		defer func() { c.hooks.StopResult(context.Background(), lastAssistantText(c.History()), turn, err) }()
	}
	ctx, marker = c.beginTurn(ctx, startMessages, !turn.synthetic && !IsSyntheticUserMessage(turn.raw))
	if continuation != nil {
		ctx = agent.WithDeliveryExecutionScope(ctx, agent.DeliveryExecutionScope{
			ID:       continuation.scopeID,
			TaskText: continuation.goal,
		})
	} else if scopeID, task, ok := c.goals.deliveryScope(); ok {
		ctx = agent.WithDeliveryExecutionScope(ctx, agent.DeliveryExecutionScope{ID: scopeID, TaskText: task})
	}
	// Goal turns bind a scope+epoch recorder for update_goal and observational
	// usage. It stays active through FSM/evaluator work; error paths clear it.
	ctx = c.bindTurnScope(ctx, continuation)
	modelInput := input
	if !turn.synthetic {
		modelInput = c.withCapabilityRoute(ctx, input, turn.raw)
	}
	ctx = c.withPlannerTurnMetadata(ctx, turn.raw, turn.synthetic)
	// Real user turns open a fresh Recovery Episode. Goal auto-continues and
	// other synthetic turns inherit the current Episode so budgets accumulate
	// only within one host-owned execution round.
	if !turn.synthetic {
		c.beginRecoveryEpisode()
	}
	err = c.runner.Run(ctx, modelInput)
	c.captureGoalRunWorkDuration(startMessages)
	c.persistGoalDeliveryCheckpoint()
	if err != nil {
		// When the user explicitly cancels, keep the real prompt and any fully
		// paired tool work. Partial reasoning/output remains durable for display
		// but is marked local-only, and a bounded recovery summary is folded into
		// the next real user turn (#5499, #6680).
		if errors.Is(err, context.Canceled) && c.CancelRequested() {
			if turn.synthetic || IsSyntheticUserMessage(turn.raw) {
				c.stripInterruptedSyntheticTurnMessagesAfter(startMessages)
			} else {
				c.stripCancelledVisibleTurnMessagesAfterWithFallback(startMessages, provider.Message{
					Role:      provider.RoleUser,
					Content:   input,
					Images:    append([]string(nil), images.userImages...),
					CreatedAt: time.Now().UnixMilli(),
				})
			}
		} else if !turn.synthetic && !IsSyntheticUserMessage(turn.raw) && c.hasInterruptedDisplayAfter(startMessages, provider.Message{
			Role: provider.RoleUser, Content: input,
		}) {
			// Provider/API failures use the same safe recovery path as an explicit
			// stop once the agent has recorded a partial stream. Completed tool
			// pairs survive; unsafe stream fragments stay local-only.
			c.stripCancelledVisibleTurnMessagesAfterWithFallback(startMessages, provider.Message{
				Role:      provider.RoleUser,
				Content:   input,
				Images:    append([]string(nil), images.userImages...),
				CreatedAt: time.Now().UnixMilli(),
			})
		}
		return err
	}
	return o.gatePlanApproval(ctx)
}

// gatePlanApproval turns a finished planning turn into the user's decision and,
// if they approve, into the execution that follows it. Each step is a lifecycle
// transition, so a decision answered after the workflow moved grants nothing.
func (o *turnOrchestrator) gatePlanApproval(ctx context.Context) error {
	c := o.c
	planning := c.plan().State()
	if planning.Phase != planmode.Planning {
		return nil
	}
	proposal := lastAssistantText(c.History())
	if proposal == "" {
		return nil // no substantive proposal to gate
	}
	// Submitting is itself a transition: from here the plan belongs to the user,
	// and anything the planner still had in flight answers to an older authority.
	submitted, ok := c.plan().Transition(planning, planmode.Submit)
	if !ok {
		return nil
	}
	c.sharePlanRuntime()
	// The plan is already visible as the assistant's answer, so the request
	// carries no subject — it's purely the gate.
	allow, _, err := c.requestApproval(ctx, approvalRequest{tool: planApprovalTool})
	if err != nil {
		return err
	}
	if !allow {
		// Back to planning, unless the card already left the workflow — in which
		// case this compare-and-move finds a state it did not expect and does
		// nothing, which is the outcome the user asked for.
		c.plan().Transition(submitted, planmode.Revise)
		c.sharePlanRuntime()
		return nil
	}
	executing, ok := c.plan().Transition(submitted, planmode.Start)
	if !ok {
		// The approval was answered after the workflow moved on. It grants
		// nothing: capability belongs to the state that is current now.
		return nil
	}
	c.sharePlanRuntime()
	defer func() {
		c.plan().Transition(executing, planmode.Exit)
		c.sharePlanRuntime()
	}()
	todoArgs := c.seedPlanTodos(proposal)
	execStart := c.sessionMessageCount()
	// Starting plan execution is a real Recovery Episode boundary even though
	// the follow-up turn is synthetic.
	c.beginRecoveryEpisode()
	// The plan is the go-ahead: don't re-prompt for each write of the approved
	// work. Auto-approve writers for the duration of this execution turn only; a
	// later turn (even "continue") falls back to the normal per-tool approval.
	c.approval.setPlanAutoApprove(true)
	defer c.approval.setPlanAutoApprove(false)
	err = func() error {
		ctx, marker := c.beginTurn(ctx, execStart, false)
		defer c.finishInFlightTurn(execStart, marker)
		return o.runComposedSyntheticTurn(ctx, planApprovedMessage)
	}()
	if err != nil {
		if errors.Is(err, context.Canceled) && c.CancelRequested() {
			c.stripInterruptedSyntheticTurnMessagesAfter(execStart)
		}
		return err
	}
	if todoArgs != "" && !c.hasTodoUpdateSince(execStart) {
		c.completePlanTodos(todoArgs)
	}
	return nil
}

func (c *Controller) captureGoalRunWorkDuration(startMessages int) {
	recorder := c.goalUsageTee.activeRecorder()
	if recorder == nil {
		return
	}
	recorder.addWorkDuration(maxRunWorkDuration(c.History(), startMessages))
	// Persist usage and duration even when the provider or host terminates this
	// Run before the FSM advances. Epoch checks reject replace/clear/resume races.
	_, _ = c.persistGoalStateAtEpoch(recorder.epoch, c.goalTodos())
}

func maxRunWorkDuration(messages []provider.Message, start int) int64 {
	if start < 0 {
		start = 0
	}
	if start > len(messages) {
		start = len(messages)
	}
	var maxDuration int64
	for _, message := range messages[start:] {
		if message.Role == provider.RoleAssistant && message.WorkDurationMs > maxDuration {
			maxDuration = message.WorkDurationMs
		}
	}
	return maxDuration
}

// runTurnLoop resolves the turn's image references, runs it, then keeps
// pursuing an active Goal with it. A turn that already carries a pinned image
// set keeps it.
func (o *turnOrchestrator) runTurnLoop(ctx context.Context, turn orchestratedTurn) error {
	return o.runTurnLoopWithPreparedTurn(ctx, o.c.prepareOrchestratedTurnImages(turn))
}

// runTurnLoopWithPreparedTurn runs the turn, then hands the outcome to the Goal
// FSM. Without an active goal advanceGoalAfterTurn returns immediately and this
// is one turn; with one it is the loop that keeps pursuing it.
func (o *turnOrchestrator) runTurnLoopWithPreparedTurn(ctx context.Context, turn orchestratedTurn) error {
	expectedContinuationEpoch := o.c.goals.continuationToken()
	ctx = o.c.withVisionRouting(agent.WithSubagentImageCandidates(ctx, turn.images.candidates))
	err := o.runOrchestratedTurn(ctx, turn)
	if err != nil {
		if ctx.Err() != nil {
			o.c.goalUsageTee.setActiveRecorder(nil)
			o.c.stopGoal(GoalStatusStopped)
			return err
		}
		if !goalTurnErrorAbsorbable(err) {
			// Terminal provider/host error: stop auto-continue.
			o.c.goalUsageTee.setActiveRecorder(nil)
			return err
		}
		if !o.c.goals.active() {
			// No Goal, but the host knows what the turn still owes. Finish it
			// rather than handing the user a card that only says "continue".
			o.c.goalUsageTee.setActiveRecorder(nil)
			return o.continueUntilReady(ctx, err)
		}
		// FinalReadinessError is absorbed below: the Goal FSM continues with
		// the missing requirements as the next turn's prompt.
	}
	return o.continueGoal(ctx, expectedContinuationEpoch, err)
}

// continueGoal runs the goal auto-continuation loop. A FinalReadinessError
// from the last turn is absorbed into the FSM decision (the Goal continues
// with the missing requirements); any other terminal error stops the loop and
// is returned to the caller.
func (o *turnOrchestrator) continueGoal(ctx context.Context, expectedContinuationEpoch uint64, firstTurnErr error) error {
	c := o.c
	turnErr := firstTurnErr
	for {
		res := o.advanceGoalAfterTurn(ctx, expectedContinuationEpoch, turnErr)
		if !res.cont {
			return nil
		}
		if err := ctx.Err(); err != nil {
			c.stopGoal(GoalStatusStopped)
			return err
		}
		intercept, ok := c.goals.acceptContinuation(res)
		if !ok {
			return nil
		}
		turn := goalContinueTurn
		if intercept != "" {
			turn = intercept
			if res.interceptNotice != "" {
				c.noticeDetail(res.interceptNotice, intercept)
			}
		}
		admitted, err := o.runGoalContinuationTurnWithRawDisplay(ctx, turn, turn, "", res)
		if err != nil {
			if ctx.Err() != nil {
				c.stopGoal(GoalStatusStopped)
				return err
			}
			if !goalTurnErrorAbsorbable(err) {
				// Terminal provider/host error: stop auto-continue; the Goal
				// stays running for the next user turn.
				c.goalUsageTee.setActiveRecorder(nil)
				return err
			}
			turnErr = err
		} else {
			turnErr = nil
		}
		if !admitted {
			return nil
		}
		expectedContinuationEpoch = res.continuationEpoch
	}
}

func goalTurnErrorAbsorbable(err error) bool {
	var readinessErr *agent.FinalReadinessError
	if errors.As(err, &readinessErr) {
		return true
	}
	_, _, ok := goalPauseFromRunError(err)
	return ok
}

func goalPauseFromRunError(err error) (cause, reason string, ok bool) {
	info, ok := agent.InspectRunPause(err)
	if !ok {
		return "", "", false
	}
	if info.Kind == "task_budget" && info.HostOwned {
		reason := strings.TrimSpace(info.Reason)
		if reason == "" {
			reason = "the Goal reached its spend budget"
		}
		return stopCauseBudgetSpend, reason, true
	}
	return "", "", false
}

// advanceGoalAfterTurn gathers every input the FSM needs off the goal lock —
// the turn's update_goal report, Delivery readiness, budget/usage state, and
// the evaluator verdict — then lets the FSM exclusively decide complete,
// continue, blocked, or pause. The usage span bound to this turn stays active
// until here so evaluator usage also counts against the goal budget.
func (o *turnOrchestrator) advanceGoalAfterTurn(ctx context.Context, expectedContinuationEpoch uint64, turnErr error) goalAdvanceResult {
	c := o.c
	recorder := c.goalUsageTee.activeRecorder()
	defer c.goalUsageTee.setActiveRecorder(nil)
	// Only active Goal turns bind a recorder. Ordinary and edited non-Goal
	// turns still pass through the shared turn wrapper, but must not enter the
	// Goal FSM or pay for an isolated completion evaluation.
	if recorder == nil || recorder.epoch != expectedContinuationEpoch ||
		!c.goals.turnActive(recorder.scopeID, recorder.epoch) {
		return goalAdvanceResult{cont: false}
	}

	var readiness agent.ReadinessResult
	var readinessErr *agent.FinalReadinessError
	pauseCause, pauseReason, runPaused := goalPauseFromRunError(turnErr)
	if errors.As(turnErr, &readinessErr) {
		readiness = agent.ReadinessResult{
			Ready:       false,
			Missing:     append([]string(nil), readinessErr.Missing...),
			Reason:      readinessErr.Reason,
			ProgressKey: readinessErr.Reason,
		}
	} else if turnErr != nil && !runPaused {
		// Terminal provider/host error: stop auto-continue without an FSM
		// transition; the goal stays running for the next user turn.
		return goalAdvanceResult{cont: false}
	} else if c.executor != nil {
		readiness = c.executor.ReadinessResult()
	}
	// The validated update_goal report for this turn, if any.
	var report *goalTurnReport
	if recorder != nil {
		report = recorder.validReport(expectedContinuationEpoch)
	}

	// The bounded evaluator runs once, only when the model gave no report and
	// readiness has no definite missing list. Failures fail closed in the FSM.
	var evaluator *goalEvaluatorVerdict
	var evaluatorFailed string
	if !runPaused && report == nil && len(readiness.Missing) == 0 {
		if c.evaluator == nil {
			evaluatorFailed = "goal evaluator unavailable"
		} else if verdict, err := c.evaluator.Evaluate(ctx, c.goalEvaluatorEvidence()); err != nil {
			evaluatorFailed = err.Error()
		} else {
			evaluator = &goalEvaluatorVerdict{outcome: verdict.Outcome, reason: verdict.Reason}
		}
	}

	var progressEvidence []string
	if c.executor != nil {
		progressEvidence = c.executor.HostProgressSignatures()
	}

	res := c.goals.advance(goalAdvanceInput{
		report:           report,
		readiness:        readiness,
		evaluator:        evaluator,
		evaluatorFailed:  evaluatorFailed,
		todos:            c.goalTodos(),
		progressEvidence: progressEvidence,
		pauseCause:       pauseCause,
		pauseReason:      pauseReason,
		expectedEpoch:    &expectedContinuationEpoch,
	})
	c.persistGoalState(res.path, res.data, res.ok)
	if res.notice != "" {
		c.notice(res.notice)
	}
	if res.notice == goalCompleteNotice && c.executor != nil {
		c.completeRemainingGoalTodos()
	}
	return res
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
