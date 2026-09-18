package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
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
	input           string
	raw             string
	imageRefs       string
	userImages      []string
	imageCandidates []string
	imagesResolved  bool
	display         string
	editedOriginal  string
	synthetic       bool
	goalRound       *goalRoundReservation
}

func newTurnOrchestrator(c *Controller) *turnOrchestrator {
	return &turnOrchestrator{c: c}
}

func (o *turnOrchestrator) runTurnWithRawDisplay(ctx context.Context, input, raw, display string) error {
	return o.runOrchestratedTurn(ctx, orchestratedTurn{input: input, raw: raw, display: display})
}

func (o *turnOrchestrator) runTurnWithImageRefsRawDisplay(ctx context.Context, input, raw, imageRefs, display string) error {
	return o.runOrchestratedTurn(ctx, orchestratedTurn{input: input, raw: raw, imageRefs: imageRefs, display: display})
}

func (o *turnOrchestrator) runSyntheticTurnWithRawDisplay(ctx context.Context, input, raw, display string) error {
	return o.runOrchestratedTurn(ctx, orchestratedTurn{input: input, raw: raw, display: display, synthetic: true})
}

func (o *turnOrchestrator) runComposedSyntheticTurn(ctx context.Context, text string) error {
	c := o.c
	ctx = agent.WithRawUserInput(ctx, text)
	ctx = withTurnInputOrigin(ctx, true)
	ctx = c.withTurnContext(ctx, false)
	ctx = c.withPlannerTurnMetadata(ctx, text, true, c.messageCount())
	return c.runModelTurn(ctx, c.ComposeSynthetic(text))
}

// runSubagentSkillGoalLoop executes a slash-invoked runAs=subagent skill as a
// real isolated child turn, then lets an active goal continue just as an inline
// skill turn did before.
func (o *turnOrchestrator) runSubagentSkillGoalLoop(ctx context.Context, sk skill.Skill, task, raw, display string, runner skill.SubagentRunner, planMode bool) error {
	return o.runSubagentSkillTurnsGoalLoop(ctx, []skill.Skill{sk}, task, raw, display, runner, planMode)
}

func (o *turnOrchestrator) runSubagentSkillTurnsGoalLoop(ctx context.Context, skills []skill.Skill, task, raw, display string, runner skill.SubagentRunner, planMode bool, frozen ...[]string) error {
	var userImages, imageCandidates []string
	if len(frozen) > 0 {
		imageCandidates = append([]string(nil), frozen[0]...)
		if o.c.imageInputEnabled() {
			userImages = append([]string(nil), imageCandidates...)
		}
	} else {
		userImages, imageCandidates = o.c.resolveTurnImages(raw)
	}
	ctx = agent.WithSubagentImageCandidates(ctx, imageCandidates)
	ctx = withPreparedTurnImages(ctx)
	return o.runSubagentSkillTurns(ctx, skills, task, raw, display, runner, planMode, userImages, imageCandidates)
}

// runSubagentSkillTurns records the composed user task and distilled child
// answers only. Child reasoning and tool chatter stay out of the
// provider-visible parent context while their UI events nest under synthetic
// top-level run_skill cards.
func (o *turnOrchestrator) runSubagentSkillTurns(ctx context.Context, skills []skill.Skill, task, raw, display string, runner skill.SubagentRunner, planMode bool, images, imageCandidates []string) (err error) {
	c := o.c
	turnStartedAt := time.Now()
	c.maybeSessionStart(ctx)
	parentSession := c.parentSessionID()
	ctx = agent.WithParentSession(ctx, parentSession)
	ctx = jobs.WithSession(ctx, parentSession)
	ctx = agent.WithUserImages(ctx, images)
	ctx = agent.WithSubagentImageCandidates(ctx, imageCandidates)
	ctx = agent.WithResponseLanguagePreference(ctx, c.responseLanguage)
	ctx = agent.WithReasoningLanguagePreference(ctx, c.reasoningLanguage)
	ctx = c.withTurnContext(ctx, true)

	input := c.compose(task, raw, true)
	startMessages := c.messageCount()
	var marker agent.InFlightTurnMeta
	defer func() { c.finishInFlightTurn(startMessages, marker) }()
	defer c.recordDisplayForNewUser(startMessages, display)
	// The checkpoint prompt labels the turn in the rewind picker (and is
	// prefilled into the composer after a conversation rewind), so it must be
	// the user's own text — never the composed provider input with its
	// transient <response-language>/<reasoning-language>/memory/hook blocks.
	c.beginCheckpoint(ctx, firstNonEmpty(raw, task))
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
		defer func() { c.hooks.StopResult(context.Background(), lastAssistantText(c.History()), turn, err) }()
	}

	marker = c.markInFlightTurn(startMessages, true)
	c.sink.Emit(event.Event{Kind: event.TurnStarted})
	if c.executor == nil {
		return fmt.Errorf("subagent slash invocation requires an active session")
	}
	message := persistedUserTurn(input, firstNonEmpty(raw, task), images, time.Now().UnixMilli())
	if prepared, ok := ctx.Value(preparedImageReferencesContextKey{}).(preparedImageReferences); ok && len(prepared.inputs) > 0 {
		message.Images = nil
		message.ImageInputs = prepared.inputs
	}
	if _, err := c.executor.AppendTurnContextAndUserChecked(ctx, message); err != nil {
		return err
	}

	for _, sk := range skills {
		sk = c.skills.prepare(sk)
		callID := fmt.Sprintf("slash-skill-%d", c.slashSkillSeq.Add(1))
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
		if err := event.EmitChecked(c.sink, event.Event{Kind: event.ToolDispatch, Tool: toolEvent}); err != nil {
			return fmt.Errorf("persist skill dispatch: %w", err)
		}
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
		messageID := agent.NewMessageID()
		assistant := provider.Message{ID: messageID, Role: provider.RoleAssistant, Content: answer, WorkDurationMs: workDurationMs}
		if err := c.RecordSessionMessages(ctx, "orchestrated-assistant", []provider.Message{assistant}); err != nil {
			return err
		}
		c.executor.Session().Add(assistant)
		display := agent.DisplayAssistantText(answer)
		c.sink.Emit(event.Event{Kind: event.Text, MessageID: messageID, Text: display})
		c.sink.Emit(event.Event{Kind: event.Message, MessageID: messageID, Text: display})
	}

	return nil
}

func (o *turnOrchestrator) runOrchestratedTurn(ctx context.Context, turn orchestratedTurn) (err error) {
	c := o.c
	c.maybeSessionStart(ctx)
	parentSession := c.parentSessionID()
	ctx = agent.WithParentSession(ctx, parentSession)
	ctx = jobs.WithSession(ctx, parentSession)
	userImages, imageCandidates := c.imagesForOrchestratedTurn(ctx, turn)
	ctx = agent.WithUserImages(ctx, userImages)
	ctx = agent.WithSubagentImageCandidates(ctx, imageCandidates)
	ctx = agent.WithRawUserInput(ctx, turn.raw)
	ctx = withPreparedTurnImages(ctx)
	ctx = withTurnInputOrigin(ctx, turn.synthetic)
	userMessageID := agent.NewMessageID()
	if _, turnID, active := c.currentTurnToken(); active {
		if receipt, ok := c.submissionForTurn(turnID); ok {
			userMessageID = receipt.MessageID
		}
	}
	if c.executor != nil {
		ctx = agent.WithUserMessageIdentity(ctx, c.executor.Session(), userMessageID)
	}
	var input string
	if turn.goalRound != nil {
		input = c.ComposeSynthetic(turn.input)
	} else {
		input = c.compose(turn.input, turn.raw, !turn.synthetic)
	}
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
	fallback := persistedUserTurn(input, turn.raw, userImages, time.Now().UnixMilli())
	fallback.ID = userMessageID
	c.noteTerminationBoundary(fallback, !turn.synthetic)
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
		defer func() { c.hooks.StopResult(context.Background(), lastAssistantText(c.History()), turn, err) }()
	}
	marker = c.markInFlightTurn(startMessages, !turn.synthetic)
	ctx = c.withTurnContext(ctx, !turn.synthetic)
	if turn.goalRound != nil {
		if authority, ok := c.goalAuthorityForRound(turn.goalRound); ok {
			ctx = tool.WithGoalLifecycle(ctx, c, authority)
		}
	} else if !turn.synthetic {
		if authority, ok := c.directHumanGoalAuthority(); ok {
			ctx = tool.WithGoalLifecycle(ctx, c, authority)
		}
	}
	ctx = c.withPlannerTurnMetadata(ctx, turn.raw, turn.synthetic, startMessages)
	modelInput := input
	if !turn.synthetic {
		modelInput = c.withCapabilityRoute(ctx, input, turn.raw)
	}
	modelInput, ctx, err = c.prepareVisionTurn(ctx, modelInput, imageCandidates)
	if err != nil {
		return err
	}
	err = c.runModelTurn(ctx, modelInput)
	if err != nil {
		fallback := persistedUserTurn(input, turn.raw, userImages, time.Now().UnixMilli())
		fallback.ID = userMessageID
		// When the user explicitly cancels, keep the real prompt and any fully
		// paired tool work. Partial reasoning/output remains durable for display
		// but is marked local-only, and a bounded recovery summary is folded into
		// the next real user turn (#5499, #6680).
		if errors.Is(err, context.Canceled) && c.CancelRequested() {
			if turn.synthetic {
				c.stripInterruptedSyntheticTurnMessagesAfter(startMessages)
			} else {
				c.stripCancelledVisibleTurnMessagesAfterWithFallback(startMessages, fallback)
			}
		} else if !turn.synthetic && c.hasInterruptedDisplayAfter(startMessages, fallback) {
			// Provider/API failures use the same safe recovery path as an explicit
			// stop once the agent has recorded a partial stream. Completed tool
			// pairs survive; unsafe stream fragments stay local-only.
			c.stripCancelledVisibleTurnMessagesAfterWithFallback(startMessages, fallback)
		}
		return err
	}
	return o.executeApprovedPlan(ctx)
}

func (o *turnOrchestrator) executeApprovedPlan(ctx context.Context) error {
	c := o.c
	c.mu.Lock()
	plan := c.sessionSettings.planMode
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
		// The host decides whether denial means "revise and keep planning" or
		// "exit without executing" by leaving plan mode on or switching it off.
		return nil
	}
	c.SetPlanMode(false)
	execStart := c.sessionMessageCount()
	// The plan is the go-ahead: don't re-prompt for each write of the approved
	// work. Auto-approve writers for the duration of this execution turn only; a
	// later turn (even "continue") falls back to the normal per-tool approval.
	c.approval.setPlanAutoApprove(true)
	defer c.approval.setPlanAutoApprove(false)
	err = func() error {
		marker := c.markInFlightTurn(execStart, false)
		defer c.finishInFlightTurn(execStart, marker)
		return o.runComposedSyntheticTurn(ctx, planApprovedMessage)
	}()
	if err != nil {
		if errors.Is(err, context.Canceled) && c.CancelRequested() {
			c.stripInterruptedSyntheticTurnMessagesAfter(execStart)
		}
		return err
	}
	return nil
}

func (o *turnOrchestrator) runGoalLoopWithRawDisplay(ctx context.Context, input, raw, display string) error {
	return o.runGoalLoopWithImageRefsRawDisplay(ctx, input, raw, "", display)
}

func (o *turnOrchestrator) runGoalLoopWithImageRefsRawDisplay(ctx context.Context, input, raw, imageRefs, display string) error {
	turn := o.c.prepareOrchestratedTurnImages(orchestratedTurn{input: input, raw: raw, imageRefs: imageRefs, display: display})
	return o.runGoalLoopWithPreparedTurn(ctx, turn)
}

func (o *turnOrchestrator) runGoalLoopWithFrozenImagesRawDisplay(ctx context.Context, input, raw, display string, images []string) error {
	turn := orchestratedTurn{
		input:           input,
		raw:             raw,
		display:         display,
		imageCandidates: append([]string(nil), images...),
		imagesResolved:  true,
	}
	if o.c.imageInputEnabled() {
		turn.userImages = append([]string(nil), images...)
	}
	return o.runGoalLoopWithPreparedTurn(ctx, turn)
}

func (o *turnOrchestrator) runGoalLoopWithPreparedTurn(ctx context.Context, turn orchestratedTurn) error {
	// Every accepted input is exactly one top-level turn. Automatic Goal work is
	// owned exclusively by goalRoundDriver after the runtime becomes idle.
	ctx = agent.WithSubagentImageCandidates(ctx, turn.imageCandidates)
	return o.runOrchestratedTurn(ctx, turn)
}

func (o *turnOrchestrator) runEditedGoalLoopWithRawDisplay(ctx context.Context, input, raw, display, original string) error {
	return o.runEditedGoalLoopWithImageRefsRawDisplay(ctx, input, raw, "", display, original)
}

func (o *turnOrchestrator) runEditedGoalLoopWithImageRefsRawDisplay(ctx context.Context, input, raw, imageRefs, display, original string) error {
	turn := o.c.prepareOrchestratedTurnImages(orchestratedTurn{
		input: input, raw: raw, imageRefs: imageRefs, display: display, editedOriginal: original,
	})
	ctx = agent.WithSubagentImageCandidates(ctx, turn.imageCandidates)
	return o.runOrchestratedTurn(ctx, turn)
}

func (o *turnOrchestrator) runEditedGoalLoopWithFrozenImagesRawDisplay(ctx context.Context, input, raw, display, original string, images []string) error {
	turn := orchestratedTurn{
		input: input, raw: raw, display: display, editedOriginal: original,
		imageCandidates: append([]string(nil), images...), imagesResolved: true,
	}
	if o.c.imageInputEnabled() {
		turn.userImages = append([]string(nil), images...)
	}
	ctx = agent.WithSubagentImageCandidates(ctx, turn.imageCandidates)
	return o.runOrchestratedTurn(ctx, turn)
}
