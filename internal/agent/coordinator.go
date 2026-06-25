package agent

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/nilutil"
	"reasonix/internal/provider"
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
actionable.`

const executorHandoffMarker = "Reasonix executor handoff"

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
	// executor instead of paying a planner round on it.
	shouldPlan func(string) bool
}

// NewCoordinator wires a planner provider (with its own session) to an executor.
// sink receives the planner's phase/text/usage events; the executor emits its
// own events to its own sink (the CLI wires the same sink into both). A nil
// sink is replaced with event.Discard.
func NewCoordinator(planner provider.Provider, plannerSession *Session, plannerPricing *provider.Pricing, plannerTools *tool.Registry, plannerOptions Options, executor *Agent, temperature float64, sink event.Sink, shouldPlan func(string) bool) *Coordinator {
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

// Run plans with the planner model, then hands the plan to the executor.
func (c *Coordinator) Run(ctx context.Context, input string) error {
	c.sink.Emit(event.Event{Kind: event.TurnStarted})
	if c.shouldPlan != nil && !c.shouldPlan(input) {
		c.sink.Emit(event.Event{Kind: event.Phase, Text: c.executor.prov.Name() + " · executing", Source: event.UsageSourceExecutor})
		return c.executor.Run(ctx, input)
	}
	c.sink.Emit(event.Event{Kind: event.Phase, Text: c.planner.Name() + " · planning", Source: event.UsageSourcePlanner})
	plan, err := c.plan(ctx, input)
	if err != nil {
		return fmt.Errorf("planner: %w", err)
	}
	c.sink.Emit(event.Event{Kind: event.Phase, Text: c.executor.prov.Name() + " · executing", Source: event.UsageSourceExecutor})
	if isNoOpPlan(plan) {
		c.persistExecutorNoOp(ctx, input, plan)
		c.sink.Emit(event.Event{Kind: event.Text, Text: plan})
		return nil
	}
	return c.executor.Run(ctx, formatHandoff(input, plan, executorToolHandoffContext(c.executor)))
}

func isNoOpPlan(plan string) bool {
	lower := strings.ToLower(strings.TrimSpace(plan))
	if lower == "" {
		return false
	}
	if containsNoOpActionTerm(lower) {
		return false
	}
	noOp := []string{
		"no changes needed",
		"no changes are needed",
		"no changes required",
		"no changes are required",
		"no action needed",
		"no action required",
		"nothing to change",
		"nothing to do",
		"already handled",
		"already implemented",
		"already resolved",
		"[no_changes]",
		"无需改动",
		"无需修改",
		"无需更改",
		"不需要修改",
		"不需要改",
		"不用改",
		"不用修改",
		"不必改动",
		"没有需要修改",
		"已经正确处理",
		"已经实现",
		"已经解决",
	}
	for _, phrase := range noOp {
		if strings.Contains(lower, phrase) && !strings.Contains(lower, "not "+phrase) && !strings.Contains(lower, "不是"+phrase) {
			return true
		}
	}
	return false
}

func containsNoOpActionTerm(lower string) bool {
	terms := []string{
		" add ", " add docs", " add tests", " update ", " edit ", " write ",
		" create ", " delete ", " remove ", " patch ", " refactor ", " implement ",
		" run ", " test ", " build ", " fix ",
		"新增", "补充", "更新", "编辑", "写入", "创建", "删除", "移除",
		"运行", "测试", "构建", "修复", "实现", "重构",
	}
	padded := " " + lower + " "
	for _, term := range terms {
		if strings.Contains(padded, term) {
			return true
		}
	}
	return false
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
	c.plannerSess.Add(provider.Message{Role: provider.RoleUser, Content: input})

	ch, err := c.planner.Stream(ctx, provider.Request{
		Messages:    c.plannerSess.Messages,
		Temperature: c.temperature,
	})
	if err != nil {
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
	before := len(c.plannerSess.Messages)
	if err := c.plannerAgent.Run(ctx, input); err != nil {
		return "", err
	}
	for i := len(c.plannerSess.Messages) - 1; i >= before; i-- {
		m := c.plannerSess.Messages[i]
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			return m.Content, nil
		}
	}
	return "", fmt.Errorf("planner finished without producing a plan")
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
	if len(toolNames) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "- The executor request includes the full tool schema (%d tools). Tool names include: %s.", len(toolNames), boundedToolNames(toolNames, 24))
	if len(mcpNames) > 0 {
		fmt.Fprintf(&b, "\n- MCP tools are already registered for the executor in this request (%d MCP tools). MCP tool names include: %s.", len(mcpNames), boundedToolNames(mcpNames, 16))
	}
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
