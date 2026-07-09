package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/nilutil"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
)

// Runner carries out one task turn. Both Agent (single model) and Coordinator
// (two-model) satisfy it, so the CLI stays agnostic to which is in use.
type Runner interface {
	Run(ctx context.Context, input string) error
}

// DefaultPlannerPrompt steers the planner toward concise plans, not execution.
const DefaultPlannerPrompt = `You are the planner in a two-model coding agent.
Given a task, produce a concise, ordered plan for the executor model to carry out.
Use the read-only tools available to you when the task needs context from the
workspace, user rules, or docs; keep that research targeted and stop once you
have enough evidence. Do not write full implementations or attempt side effects.
Do not ask the user how to trigger the executor and do not say you are waiting
for the executor. Output executor-ready instructions: what to do, which files or
commands are relevant, expected blockers, and key decisions. Keep it short and
actionable.

Crucial: You only have read-only tools. You do NOT have bash, execute, or
side-effect tools — those belong to the executor. Never question or dwell
on the lack of execution tools; it is by design. Just plan what the executor
should do with its tools.

If your research shows the task needs no changes and no actions at all (already
implemented, already resolved), explain that briefly and end your reply with a
final line containing exactly [no_changes]. Never emit that marker when any
work, verification, or follow-up remains.`

const executorHandoffMarker = "Reasonix executor handoff"

// plannerFallbackNotice is shown when the planner fails and the turn degrades
// to executor-only instead of failing outright.
const plannerFallbackNotice = "Planner failed; continuing this turn with the executor only."

// noChangesMarker is the explicit no-op conclusion the planner is asked to emit
// on its final line (see DefaultPlannerPrompt). isNoOpPlan trusts it over the
// legacy phrase heuristics.
const noChangesMarker = "[no_changes]"

// PlannerPromptWithContext appends cache-stable standing context, such as loaded
// REASONIX.md / AGENTS.md memory, to the planner's smaller system prompt.
func PlannerPromptWithContext(context string) string {
	context = strings.TrimSpace(context)
	if context == "" {
		return DefaultPlannerPrompt
	}
	return DefaultPlannerPrompt + "\n\n# Planning context\n\n" + context
}

// Coordinator runs two models in separate sessions to keep each one's prompt
// prefix cache-stable: a low-frequency planner proposes an approach, then the
// executor (a full tool-using Agent) carries it out. The sessions never mix, so
// neither model's prefix is disturbed by the other's turns.
type Coordinator struct {
	planner        provider.Provider
	plannerSess    *Session
	plannerSystem  string
	plannerPricing *provider.Pricing
	plannerAgent   *Agent
	executor       *Agent
	temperature    float64
	sink           event.Sink
	// shouldPlan gates the planner pass per turn; nil plans every turn. Lets a
	// trivial, non-work turn (a question, a greeting) skip straight to the
	// executor instead of paying a planner round on it. The turn context is
	// passed through so a classifier-backed gate stops with the turn instead
	// of running out its own timeout after the user cancels.
	shouldPlan func(context.Context, string) bool
}

// NewCoordinator wires a planner provider (with its own session) to an executor.
// sink receives the planner's phase/text/usage events; the executor emits its
// own events to its own sink (the CLI wires the same sink into both). A nil
// sink is replaced with event.Discard.
func NewCoordinator(planner provider.Provider, plannerSession *Session, plannerPricing *provider.Pricing, plannerTools *tool.Registry, plannerOptions Options, executor *Agent, temperature float64, sink event.Sink, shouldPlan func(context.Context, string) bool) *Coordinator {
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	if plannerSession == nil {
		plannerSession = NewSession("")
	}
	plannerSystem := sessionSystemPrompt(plannerSession)
	var plannerAgent *Agent
	if plannerTools != nil {
		plannerOptions.Temperature = temperature
		plannerOptions.Pricing = plannerPricing
		plannerOptions.UsageSource = event.UsageSourcePlanner
		plannerAgent = New(planner, plannerTools, plannerSession, plannerOptions, plannerSink(sink))
	}
	if executor != nil {
		executor.executorHandoffGuard = true
	}
	return &Coordinator{
		planner:        planner,
		plannerSess:    plannerSession,
		plannerSystem:  plannerSystem,
		plannerPricing: plannerPricing,
		plannerAgent:   plannerAgent,
		executor:       executor,
		temperature:    temperature,
		sink:           sink,
		shouldPlan:     shouldPlan,
	}
}

func sessionSystemPrompt(s *Session) string {
	if s == nil {
		return ""
	}
	for _, m := range s.Snapshot() {
		if m.Role == provider.RoleSystem {
			return m.Content
		}
	}
	return ""
}

// ResetPlannerSession discards turn-local planner history when the owning
// controller moves to a different executor session. Saved transcripts only
// persist executor-visible conversation; carrying the old planner transcript
// into a new/resumed session can make the next plan reuse unrelated tasks.
func (c *Coordinator) ResetPlannerSession() {
	if c == nil {
		return
	}
	system := c.plannerSystem
	if system == "" {
		system = sessionSystemPrompt(c.plannerSess)
	}
	next := NewSession(system)
	c.plannerSess = next
	if c.plannerAgent != nil {
		c.plannerAgent.SetSession(next)
	}
}

// SetReasoningLanguage updates both agents in two-model mode. The raw planner
// path receives controller-composed input directly, but a tool-enabled planner
// owns its own Agent and must clear stale zh/en preferences on live changes.
func (c *Coordinator) SetReasoningLanguage(lang string) {
	if c == nil {
		return
	}
	if c.plannerAgent != nil {
		c.plannerAgent.SetReasoningLanguage(lang)
	}
	if c.executor != nil {
		c.executor.SetReasoningLanguage(lang)
	}
}

// SetResponseLanguage updates both agents in two-model mode.
func (c *Coordinator) SetResponseLanguage(lang string) {
	if c == nil {
		return
	}
	if c.plannerAgent != nil {
		c.plannerAgent.SetResponseLanguage(lang)
	}
	if c.executor != nil {
		c.executor.SetResponseLanguage(lang)
	}
}

// SetPlanMode propagates the read-only gate to both planner and executor agents
// in two-model mode. Callers that only set the controller's executor would miss
// the planner agent inside the Coordinator, causing stale plan-mode state after
// approvals or manual mode switches.
func (c *Coordinator) SetPlanMode(v bool) {
	if c == nil {
		return
	}
	if c.plannerAgent != nil {
		c.plannerAgent.SetPlanMode(v)
	}
	if c.executor != nil {
		c.executor.SetPlanMode(v)
	}
}

// SetPlanModeReadOnlyTrustGate propagates MCP read-only trust approvals to both
// tool-using agents in two-model mode.
func (c *Coordinator) SetPlanModeReadOnlyTrustGate(g PlanModeReadOnlyTrustGate) {
	if c == nil {
		return
	}
	if c.plannerAgent != nil {
		c.plannerAgent.SetPlanModeReadOnlyTrustGate(g)
	}
	if c.executor != nil {
		c.executor.SetPlanModeReadOnlyTrustGate(g)
	}
}

// SetSandboxEscapeApprover propagates one-shot shell sandbox escape approvals to
// both tool-using agents in two-model mode.
func (c *Coordinator) SetSandboxEscapeApprover(g sandbox.EscapeApprover) {
	if c == nil {
		return
	}
	if c.plannerAgent != nil {
		c.plannerAgent.SetSandboxEscapeApprover(g)
	}
	if c.executor != nil {
		c.executor.SetSandboxEscapeApprover(g)
	}
}

// SetConfigWriteApprover propagates Reasonix-managed config write approvals to
// both tool-using agents in two-model mode.
func (c *Coordinator) SetConfigWriteApprover(g tool.ConfigWriteApprover) {
	if c == nil {
		return
	}
	if c.plannerAgent != nil {
		c.plannerAgent.SetConfigWriteApprover(g)
	}
	if c.executor != nil {
		c.executor.SetConfigWriteApprover(g)
	}
}

// Run plans with the planner model, then hands the plan to the executor.
func (c *Coordinator) Run(ctx context.Context, input string) error {
	c.sink.Emit(event.Event{Kind: event.TurnStarted})
	if c.shouldPlan != nil && !c.shouldPlan(ctx, input) {
		c.sink.Emit(event.Event{Kind: event.Phase, Text: c.executor.prov.Name() + " · executing", Source: event.UsageSourceExecutor})
		return c.executor.Run(ctx, input)
	}
	c.sink.Emit(event.Event{Kind: event.Phase, Text: c.planner.Name() + " · planning", Source: event.UsageSourcePlanner})
	plan, err := c.plan(ctx, input)
	if err != nil {
		// Cancellation and max-steps pauses are control flow, not planner
		// failures: the first is the user aborting the turn, the second has
		// saved work in the planner session and asks the user to continue.
		// Neither may silently restart on the executor.
		var pause *maxStepsPause
		if ctx.Err() != nil || errors.As(err, &pause) {
			return fmt.Errorf("planner: %w", err)
		}
		// A planner failure must not take down the turn: the executor is
		// healthy and owns the full tool set, so degrade to single-model for
		// this turn (mirroring the auto-plan classifier's fallback to the
		// heuristic when it errors).
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: plannerFallbackNotice, Detail: "planner failed; running the executor without a plan: " + err.Error(), Source: event.UsageSourcePlanner})
		c.sink.Emit(event.Event{Kind: event.Phase, Text: c.executor.prov.Name() + " · executing", Source: event.UsageSourceExecutor})
		return c.executor.Run(ctx, input)
	}
	c.sink.Emit(event.Event{Kind: event.Phase, Text: c.executor.prov.Name() + " · executing", Source: event.UsageSourceExecutor})
	if isNoOpPlan(plan) {
		c.persistExecutorNoOp(ctx, input, plan)
		// The relayed conclusion is planner text; keep its source so sinks
		// attribute it like every other planner emission.
		c.sink.Emit(event.Event{Kind: event.Text, Text: plan, Source: event.UsageSourcePlanner})
		return nil
	}
	return c.executor.Run(ctx, formatHandoff(input, plan, executorToolHandoffContext(c.executor)))
}

// isNoOpPlan reports whether the plan explicitly concludes that nothing needs
// to change: the final non-empty line is exactly the [no_changes] marker that
// DefaultPlannerPrompt requests. The marker is trusted as-is, so research notes
// above it (which may mention tests, runs, or edits that already exist) cannot
// veto the conclusion. There is deliberately no phrase heuristic behind it: a
// wrong skip silently drops the task, while a planner that ignores the marker
// contract just costs one executor round.
func isNoOpPlan(plan string) bool {
	return strings.ToLower(lastNonEmptyLine(plan)) == noChangesMarker
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

func (c *Coordinator) persistExecutorNoOp(ctx context.Context, input, plan string) {
	if c == nil || c.executor == nil || c.executor.session == nil {
		return
	}
	c.executor.session.Add(provider.Message{Role: provider.RoleUser, Content: c.executor.withTurnPreferences(input), Images: userImages(ctx)})
	c.executor.session.Add(provider.Message{Role: provider.RoleAssistant, Content: plan})
}

// plan streams a plan from the planner and appends it to the planner session, so
// that session grows prepend-only and stays cache-friendly.
func (c *Coordinator) plan(ctx context.Context, input string) (string, error) {
	if c.plannerAgent != nil {
		return c.planWithTools(ctx, input)
	}
	// On failure, roll the just-added user message back: a dangling user turn
	// would produce consecutive user roles on the next plan (which some
	// providers reject), and Run's executor fallback keeps the turn alive
	// after this error, so the planner session must stay coherent.
	before := c.plannerSess.Snapshot()
	c.plannerSess.Add(provider.Message{Role: provider.RoleUser, Content: input})

	ch, err := c.planner.Stream(ctx, provider.Request{
		Messages:    c.plannerSess.Messages,
		Temperature: provider.OptionalTemperature(c.temperature),
	})
	if err != nil {
		c.plannerSess.Replace(before)
		return "", err
	}

	var text strings.Builder
	var usage *provider.Usage
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
			c.sink.Emit(event.Event{Kind: event.Text, Text: chunk.Text, Source: event.UsageSourcePlanner})
		case provider.ChunkUsage:
			usage = chunk.Usage
		case provider.ChunkError:
			c.plannerSess.Replace(before)
			return "", chunk.Err
		}
	}
	// Closes the planner's raw text block (no markdown redraw) and prints its
	// usage line, mirroring the old Fprintln + printUsage tail.
	c.sink.Emit(event.Event{Kind: event.Usage, Usage: usage, Pricing: c.plannerPricing, Source: event.UsageSourcePlanner, UsageSource: event.UsageSourcePlanner})

	plan := text.String()
	c.plannerSess.Add(provider.Message{Role: provider.RoleAssistant, Content: plan})
	return plan, nil
}

// planWithTools runs the planner through the normal Agent loop over a filtered
// read-only registry. That gives the planner the same tool-call contract as the
// executor while preserving its separate session and cache prefix.
func (c *Coordinator) planWithTools(ctx context.Context, input string) (string, error) {
	before := c.plannerSess.Snapshot()
	rewriteBefore := c.plannerSess.RewriteVersion()
	if err := c.plannerAgent.Run(ctx, input); err != nil {
		// Mirror plan()'s rollback: Run already appended the user message
		// (and possibly partial assistant/tool rounds) to the planner
		// session, and Coordinator.Run degrades to the executor on planner
		// failure, so a dangling user message would produce consecutive
		// user roles on the next plan. A max-steps pause is exempt: its
		// saved work is what the user is asked to continue from.
		var pause *maxStepsPause
		if !errors.As(err, &pause) {
			c.rollbackPlannerTurn(before, rewriteBefore)
		}
		return "", err
	}
	// The plan is this turn's final answer: the last non-empty assistant
	// message appended after the pre-turn boundary. When a session rewrite
	// landed during the turn (auto-compaction fires right after the final
	// answer), the pre-turn length no longer maps to a boundary in the
	// rewritten log — it can even exceed it, hiding a successfully produced
	// plan. Rewrites keep the recent tail verbatim, so scanning the whole
	// rewritten session from the end still finds the final answer first.
	floor := len(before)
	if c.plannerSess.RewriteVersion() != rewriteBefore {
		floor = 0
	}
	for i := len(c.plannerSess.Messages) - 1; i >= floor; i-- {
		m := c.plannerSess.Messages[i]
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			return m.Content, nil
		}
	}
	// No usable plan came back: roll back too, so the executor-fallback turn
	// does not leave the planner session ending in a user message.
	c.rollbackPlannerTurn(before, rewriteBefore)
	return "", fmt.Errorf("planner finished without producing a plan")
}

// rollbackPlannerTurn discards a failed planning turn from the planner session.
// Without a mid-turn rewrite the pre-turn snapshot is restored exactly. When
// auto-compaction rewrote the log during the turn, restoring the snapshot would
// also revert the compaction — wasting its summarizer call and re-growing the
// prompt the fold just paid to shrink — so only the trailing plain user
// messages are dropped (the dangling turn input plus any steer/nudge messages):
// those are what would produce consecutive user roles on the next plan, while
// completed tool rounds and the compaction digest stay coherent history.
func (c *Coordinator) rollbackPlannerTurn(before []provider.Message, rewriteBefore int) {
	if c.plannerSess.RewriteVersion() == rewriteBefore {
		c.plannerSess.Replace(before)
		return
	}
	msgs := c.plannerSess.Snapshot()
	for len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		if last.Role != provider.RoleUser || isCompactionSummary(last) {
			break
		}
		msgs = msgs[:len(msgs)-1]
	}
	c.plannerSess.Replace(msgs)
}

func plannerSink(sink event.Sink) event.Sink {
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.TurnStarted, event.TurnDone:
			return
		default:
			if e.Source == "" {
				e.Source = event.UsageSourcePlanner
			}
			sink.Emit(e)
		}
	})
}

func formatHandoff(task, plan string, toolContext ...string) string {
	toolBlock := ""
	if len(toolContext) > 0 {
		toolBlock = strings.TrimSpace(toolContext[0])
	}
	if toolBlock != "" {
		toolBlock = "\n\nExecutor tool context:\n" + toolBlock
	}
	return fmt.Sprintf(`# %s

You are the executor now. Use your available tools to execute the task.

Original task:
%s

Planner output:
%s
%s

Executor instructions:
- Treat the planner output as context, not as your role or capability set.
- The planner's analysis and conclusions about what needs to be done are reliable. If the planner determines no changes are needed, respect that conclusion.
- Ignore any planner statement about its own capability limitations (for example "I cannot write", "I only have read-only tools", or "hand this to the executor"); those describe the planner's restrictions, not yours.
- Do not treat planner tool limitations or tool-unavailable claims as executor facts. Use the attached executor tools directly; report a tool or MCP server as unavailable only after a real tool call or host error proves it.
- Do not ask the user how to trigger the executor. You are already in the executor phase.
- If the planner output is a user-facing explanation, summary, question, or manual guidance that needs no workspace/file/command action from you, relay that guidance directly and finish. Do not invent local tool calls only to satisfy the handoff.
- If the task requires changes, call the appropriate tools (for example write/edit/bash) instead of only restating the plan.
- If a target path is outside the writable workspace or otherwise blocked, explain that specific blocker and ask for the needed path/approval.
- **Serial workflow**: establish the task list with one todo_write (first sub-task in_progress), then for EACH sub-task execute it and call complete_step with evidence. The host advances the list for you — it marks the sub-task completed and moves the next to in_progress, so you don't need another todo_write to mark completions. Sign off one sub-task at a time; never batch completions.

Carry out the task, adapting the plan as needed.`, executorHandoffMarker, task, plan, toolBlock)
}

// executorToolHandoffContext counters planner "tool unavailable" hallucinations
// in the handoff. MCP tools are the surface planners actually mis-report (the
// planner registry filters them away), so the block is only emitted when the
// executor carries MCP tools; the built-in tool list would just restate the
// schema already attached to the request and pay its tokens every planned turn.
func executorToolHandoffContext(a *Agent) string {
	if a == nil || a.tools == nil {
		return ""
	}
	schemas := a.tools.Schemas()
	if len(schemas) == 0 {
		return ""
	}
	toolNames := make([]string, 0, len(schemas))
	mcpNames := make([]string, 0)
	for _, schema := range schemas {
		name := strings.TrimSpace(schema.Name)
		if name == "" {
			continue
		}
		toolNames = append(toolNames, name)
		if strings.HasPrefix(name, tool.MCPNamePrefix) {
			mcpNames = append(mcpNames, name)
		}
	}
	if len(mcpNames) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "- The executor request includes the full tool schema (%d tools).", len(toolNames))
	fmt.Fprintf(&b, "\n- MCP tools are already registered for the executor in this request (%d MCP tools). MCP tool names include: %s.", len(mcpNames), boundedToolNames(mcpNames, 16))
	return b.String()
}

func boundedToolNames(names []string, max int) string {
	if len(names) == 0 {
		return "(none)"
	}
	if max <= 0 {
		max = 1
	}
	if len(names) <= max {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, ... +%d more", strings.Join(names[:max], ", "), len(names)-max)
}

// HandoffTask returns the original user task embedded in an executor handoff
// message, or s unchanged when it is not one. Session previews and auto-titles
// use it so dual-model sessions surface the user's words, not the handoff
// boilerplate (#3860).
func HandoffTask(s string) string {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "# "+executorHandoffMarker) {
		return s
	}
	const header = "Original task:\n"
	i := strings.Index(trimmed, header)
	if i < 0 {
		return s
	}
	rest := trimmed[i+len(header):]
	if j := strings.Index(rest, "\n\nPlanner output:"); j >= 0 {
		rest = rest[:j]
	}
	if task := strings.TrimSpace(rest); task != "" {
		return task
	}
	return s
}
