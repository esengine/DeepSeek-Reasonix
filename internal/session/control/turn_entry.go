package control

// Where a turn begins: the verbs a frontend calls to send input, and the loop
// carrying one turn through the runner. What a running turn does lives in the
// turn_* files beside this one.

import (
	"context"
	"errors"
	"fmt"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/langpref"
	"reasonix/internal/tools/jobs"
)

func turnOutcome(err error) string {
	var readinessErr *agent.FinalReadinessError
	if errors.As(err, &readinessErr) {
		return event.TurnOutcomeFinalReadiness
	}
	if info, ok := agent.InspectRunPause(err); ok &&
		(info.Kind == agent.PauseKindNoProgress || info.Kind == agent.PauseKindPerseveration) {
		return event.TurnOutcomeNoProgress
	}
	return ""
}

// Send starts a turn with an uncomposed message. The controller applies
// plan-mode, memory, and background-job framing inside the async turn path.
func (c *Controller) Send(input string) {
	c.SendWithRaw(input, input)
}

// SendWithRaw starts a turn with separate model input and raw prompt text.
func (c *Controller) SendWithRaw(input, raw string) {
	c.runGuarded(func(ctx context.Context) error {
		return c.runTurnLoop(ctx, orchestratedTurn{input: input, raw: raw})
	})
}

// runTurnLoop runs one model turn under the plan-approval gate, then keeps
// pursuing an active Goal with it — with no goal the loop is what a single turn
// looks like, plus whatever that turn still owes. In Plan the model writes its
// plan as an ordinary answer; approving it exits plan mode and continues into
// execution, rejecting it leaves the next turn free to revise.
func (c *Controller) runTurnLoop(ctx context.Context, turn orchestratedTurn) error {
	if err := c.leaveForeignSession(); err != nil {
		return err
	}
	return newTurnOrchestrator(c).runTurnLoop(ctx, turn)
}

// runOneTurn runs a single model turn with no Goal loop behind it.
func (c *Controller) runOneTurn(ctx context.Context, turn orchestratedTurn) error {
	if err := c.leaveForeignSession(); err != nil {
		return err
	}
	return newTurnOrchestrator(c).runOrchestratedTurn(ctx, turn)
}

// RunTurn executes one foreground turn synchronously through the same lifecycle
// used by interactive frontends: transient memory/background-job
// composition, checkpoints, hooks, and plan approval. It is for transports that
// need a blocking request/response boundary, such as ACP session/prompt.
func (c *Controller) RunTurn(ctx context.Context, input string) error {
	return c.RunTurnWithRaw(ctx, input, input, "")
}

// RunTurnWithRaw runs a blocking turn with the user's text separate from the composed input.
func (c *Controller) RunTurnWithRaw(ctx context.Context, input, raw, invokedSkill string) error {
	return c.runSynchronousTurn(ctx, nil, func(runCtx context.Context) error {
		if invokedSkill != "" {
			runCtx = withInvokedSkills(runCtx, []string{invokedSkill})
		}
		return c.runTurnLoop(runCtx, orchestratedTurn{input: input, raw: raw})
	})
}

// turnTags are what one submission carries into the turn it becomes: its
// output format and the paired device it came from. They ride the turn's
// context, never a controller slot, so two requests cannot trade them.
type turnTags struct {
	format string
	via    *provider.Via
	// refuseUnknownSlash answers a slash command nothing resolves with a notice
	// instead of sending the line to the model as prose.
	refuseUnknownSlash bool
}

type turnViaKey struct{}

// withTurnTags binds a submission's tags to the turn context.
func (c *Controller) withTurnTags(ctx context.Context, tags turnTags) context.Context {
	return withTurnVia(c.withTurnFormat(ctx, tags.format), tags.via)
}

func withTurnVia(ctx context.Context, via *provider.Via) context.Context {
	if via == nil {
		return ctx
	}
	return context.WithValue(ctx, turnViaKey{}, via)
}

// turnVia is the paired device the turn's message came from, nil for the
// window.
func turnVia(ctx context.Context) *provider.Via {
	via, _ := ctx.Value(turnViaKey{}).(*provider.Via)
	return via
}

// withTurnFormat binds a structured-output format to the turn context
// (empty is a no-op). Extracted from the runTurnLoop closure so tests can
// assert the format actually reaches the agent request path.
func (c *Controller) withTurnFormat(ctx context.Context, format string) context.Context {
	if format == "" {
		return ctx
	}
	return agent.WithResponseFormat(ctx, format)
}

func (c *Controller) runSubagentSkillSlash(sk skill.Skill, task, raw, display string) {
	sk = c.skills.prepare(sk)
	c.runGuarded(func(ctx context.Context) error {
		planMode := c.PlanMode()
		runner := c.skillRunner
		if runner == nil {
			return fmt.Errorf("subagent skill runner is unavailable for /%s", sk.Name)
		}
		return newTurnOrchestrator(c).runSubagentSkillGoalLoop(ctx, sk, task, raw, display, runner, planMode)
	})
}

// lastAssistantText returns the content of the most recent assistant message with
// non-empty text — the model's final answer for the turn (its plan, in plan mode).
func lastAssistantText(msgs []provider.Message) string {
	for _, msg := range slices.Backward(msgs) {
		if msg.Role == provider.RoleAssistant && strings.TrimSpace(msg.Content) != "" {
			return msg.Content
		}
	}
	return ""
}

// IsNonTurnInput reports input that has no turn to start: a management verb, a
// memory note, a shell shortcut. A frontend that judges a submission by whether
// a turn began has to ask this first — /compact does its work and emits a
// notice without ever running one.
func IsNonTurnInput(input string) bool { return isNonTurnHTTPInput(input) }

// isNonTurnHTTPInput reports inputs that never reach the agent turn loop, so a
// structured-output request attached to them would otherwise leak into the
// next real turn (the format slot is consumed only by runTurnLoopWithRawDisplay).
func isNonTurnHTTPInput(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return true
	}
	// Memory quick-add / remember shortcuts and goal commands bypass turns.
	if _, ok := MemoryQuickAddNote(trimmed); ok {
		return true
	}
	if _, ok := RememberCommandNote(trimmed); ok {
		return true
	}
	// "!" shell commands are rejected by submitHTTP before the turn loop
	// (403 over HTTP); a format attached to them would never be consumed.
	if strings.HasPrefix(trimmed, "!") {
		return true
	}
	// Slash commands are management verbs (/compact /new /clear /model ...)
	// or notices, not completion turns.
	if strings.HasPrefix(trimmed, "/") {
		return true
	}
	return false
}

// runRefTurn resolves the turn's @references into a context block and starts a
// turn with it prepended (or the raw line when nothing resolved), under turn
// admission.
func (c *Controller) runRefTurn(r refTurn) {
	c.runGuarded(func(ctx context.Context) error { return c.runRefTurnSync(ctx, r) })
}

// runRefTurnSync is runRefTurn on the caller's goroutine, for a caller that
// already holds turn admission.
func (c *Controller) runRefTurnSync(ctx context.Context, r refTurn) error {
	ctx = c.withTurnTags(ctx, r.tags)
	resolve := r.resolve
	if resolve == nil {
		resolve = c.ResolveRefs
	}
	refLine := r.refLine
	if refLine == "" {
		refLine = r.input
	}
	block, errs := resolve(ctx, refLine)
	for _, e := range errs {
		c.notice(e)
	}
	sent := r.input
	if block != "" {
		sent = "Referenced context:\n\n" + block + "\n\n" + r.input
	}
	return c.runTurnLoop(ctx, orchestratedTurn{
		input: sent, raw: r.input, imageRefs: refLine, display: r.display, editedOriginal: r.original,
	})
}

// runReady is the body of the headless `reasonix run` turn, where the Sink
// renders to stdout and the caller just needs the exit status — no TurnDone
// event. Run admits it through runSynchronousTurn.
func (c *Controller) runReady(ctx context.Context, input string) (err error) {
	ctx = extension.ContextWithRuntimeOwner(ctx, c.RuntimeOwner())
	if c.RuntimePhase() == RuntimePhaseDraining {
		c.emitDrainingNotice()
		return ErrRuntimeDraining
	}
	c.maybeSessionStart(ctx)
	parentSession := c.parentSessionID()
	ctx = agent.WithParentSession(ctx, parentSession)
	ctx = jobs.WithSession(ctx, parentSession)
	rawInput := input
	ctx, turnImgs := c.withTurnImages(ctx, rawInput)
	ctx = agent.WithRawUserInput(ctx, rawInput)
	input = c.imageRoutingPrefix(turnImgs) + c.Compose(input)
	// input.receive: same interception seam as the orchestrated turn — the
	// composed headless input crosses the extension chain before it enters
	// the session.
	input, blocked, interceptErr := c.interceptInputReceive(ctx, input)
	if interceptErr != nil {
		return interceptErr
	}
	if blocked {
		return nil
	}
	startMessages := c.messageCount()
	var marker sessionstore.InFlightTurnMeta
	defer func() { c.finishInFlightTurn(startMessages, marker) }()
	if err := c.beginCheckpoint(ctx, input); err != nil {
		return err
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
	ctx, marker = c.beginTurn(ctx, startMessages, true)
	ctx = c.announceAuthoredTurn(ctx, rawInput, startMessages)
	ctx = c.withPlannerTurnMetadata(ctx, rawInput, false)
	err = c.runSettled(ctx, c.withCapabilityRoute(ctx, input, rawInput))
	return err
}

// RunSubagentProfile executes one named runAs=subagent skill synchronously and
// returns only its final answer. It is the headless CLI counterpart to explicit
// slash invocation: the child keeps an isolated session, while the caller owns
// stdout rendering and exit status. readOnly selects the preview-safe runner
// used by `reasonix subagent try`.
func (c *Controller) RunSubagentProfile(ctx context.Context, name, task string, readOnly bool) (string, error) {
	name = strings.TrimSpace(name)
	task = strings.TrimSpace(task)
	if name == "" {
		return "", fmt.Errorf("subagent name is required")
	}
	if task == "" {
		return "", fmt.Errorf("subagent task is required")
	}
	sk, ok := c.skills.bySlashName(name)
	if !ok {
		return "", fmt.Errorf("unknown or disabled subagent profile %q", name)
	}
	if sk.RunAs != skill.RunSubagent {
		return "", fmt.Errorf("skill %q is not runAs=subagent", name)
	}
	sk = c.skills.prepare(sk)
	runner := c.skillRunner
	if readOnly {
		runner = c.readOnlySkillRunner
	}
	if runner == nil {
		return "", fmt.Errorf("subagent skill runner is unavailable for %q", name)
	}

	c.maybeSessionStart(ctx)
	parentSession := c.parentSessionID()
	ctx = agent.WithParentSession(ctx, parentSession)
	ctx = jobs.WithSession(ctx, parentSession)
	ctx, turnImgs := c.withTurnImages(ctx, task)
	ctx = langpref.WithResponseLanguagePreference(ctx, c.display.responseLanguage)
	ctx = langpref.WithReasoningLanguagePreference(ctx, c.display.reasoningLanguage)
	ctx = agent.WithSubagentDepth(ctx, 0)
	answer, err := runner(ctx, sk, c.imageRoutingPrefix(turnImgs)+task, skill.SubagentRunOptions{HostInitiated: true})
	if err != nil {
		return "", err
	}
	return tool.GuardSubagentHostDecisionText(answer), nil
}

func (p plannerPlanApprover) RunWithPlannerApproval(ctx context.Context, plan string, run func(context.Context) error) error {
	c := p.c
	allow, _, err := c.requestApproval(ctx, approvalRequest{tool: planApprovalTool, reason: "Planner requested host approval before execution."})
	if err != nil {
		return err
	}
	if !allow {
		return nil
	}
	todoArgs := c.seedPlanTodos(plan)
	execStart := c.sessionMessageCount()
	c.approval.setPlanAutoApprove(true)
	defer c.approval.setPlanAutoApprove(false)
	if err := run(ctx); err != nil {
		return err
	}
	if todoArgs != "" && !c.hasTodoUpdateSince(execStart) {
		c.completePlanTodos(todoArgs)
	}
	return nil
}
