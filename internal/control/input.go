package control

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"reasonix/internal/agent"
	"reasonix/internal/planmode"
	"reasonix/internal/skill"
)

// InvocationRequest is an explicit user-selected Skill or Subagent entity.
// Offset is used only to preserve the visual order chosen in the composer.
type InvocationRequest struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Offset int    `json:"offset"`
}

// PlanModeMarker is prepended to every user turn while plan mode is on. It rides
// in the user message (not the system prompt or tools), so the cache-stable
// prompt prefix is left untouched and the toggle costs nothing in cache hits.
const PlanModeMarker = planmode.Marker

const legacyPlanModeMarker = "[Plan mode — read-only. Explore the codebase first (read_file, ls, grep, glob, web_fetch, task, ask are available; writers are refused by the harness). Before planning, if a decision that is genuinely the user's — tech stack, an ambiguous requirement, scope, an irreversible choice — would materially shape the plan and you can't settle it from the codebase or a sensible default, use the ask tool to clarify it first; otherwise pick the obvious default and state the assumption in the plan instead of asking. Then present a LAYERED plan as your reply and stop — do not write files, edit, or run side-effecting bash. Structure the plan as a two-level markdown list so it becomes a layered task list: each PHASE is a top-level numbered list item (a coherent milestone, e.g. \"1. Add the config loader\"), and each phase's concrete, verifiable sub-steps are bullets indented beneath it (e.g. \"   - parse the TOML into Config\"). Use plain numbered list items for phases — do NOT write phases as markdown headings (##, ###) — so both levels parse. Keep phases few (about 2-6). The user will be asked to approve before any changes are made.]"

const (
	activeGoalOpen  = "<active-goal>"
	activeGoalClose = "</active-goal>"
	hookContextTag  = "hook-context"
)

const (
	maxHookContextChars      = 10000
	maxTotalHookContextChars = 20000
)

const (
	GoalStatusRunning  = "running"
	GoalStatusComplete = "complete"
	GoalStatusBlocked  = "blocked"
	GoalStatusStopped  = "stopped"
)

type GoalResearchMode int

const (
	GoalResearchAuto GoalResearchMode = iota
	GoalResearchOn
	GoalResearchOff
)

// StripComposePrefixes removes controller-injected prefixes from a composed
// user message so that the display text matches what the user actually typed.
// It strips the PlanModeMarker plus transient XML blocks such as
// <reasoning-language>, <memory-update>, and <background-jobs> that Compose
// prepends to user turns. This is used as a fallback when no .display.json
// sidecar recording exists (e.g. sessions created before the display-recording
// feature, or synthetic user messages injected by the controller).
func StripComposePrefixes(content string) string {
	s := agent.StripTransientUserBlocks(content)
	s = stripComposeMarker(s, PlanModeMarker)
	s = stripComposeMarker(s, legacyPlanModeMarker)
	s = strings.TrimSpace(s)
	return s
}

func stripComposeMarker(s, marker string) string {
	s = strings.TrimPrefix(s, marker+"\n\n")
	return strings.TrimPrefix(s, marker)
}

// StripReferencedContextPrefix removes the "Referenced context:" preamble and
// the trailing XML reference blocks (<file>, <dir>, <resource>, <image>) that
// controller.ResolveRefs injects when the user @-references files or resources.
// The user's actual input follows the reference blocks after a blank line.
// Used for title generation and previews so the displayed text matches what
// the user typed, not the injected context preamble (#4954).
func StripReferencedContextPrefix(content string) string {
	const preamble = "Referenced context:"
	s := strings.TrimSpace(content)
	if !strings.HasPrefix(s, preamble) {
		return content
	}
	// Skip past the preamble.
	s = strings.TrimSpace(s[len(preamble):])
	// Skip past all XML reference blocks: <file ...>...</file>, <dir ...>...</dir>,
	// <resource ...>...</resource>, <image ...>...</image>.
	for {
		s = strings.TrimSpace(s)
		if s == "" {
			return ""
		}
		// Check for a reference block start.
		if !strings.HasPrefix(s, "<file ") && !strings.HasPrefix(s, "<dir ") &&
			!strings.HasPrefix(s, "<resource ") && !strings.HasPrefix(s, "<image ") {
			break
		}
		// Find the matching close tag.
		tagEnd := strings.IndexByte(s, ' ')
		if tagEnd < 0 {
			break
		}
		tagName := s[1:tagEnd]
		closeTag := "</" + tagName + ">"
		closeIdx := strings.Index(s, closeTag)
		if closeIdx < 0 {
			break
		}
		s = strings.TrimSpace(s[closeIdx+len(closeTag):])
	}
	return s
}

// IsSyntheticUserMessage returns true if the content matches one of the known
// synthetic user messages injected by the controller or agent loop (plan
// approval, stream recovery, readiness retry, etc.). These should not be shown
// in the chat UI.
func IsSyntheticUserMessage(content string) bool {
	trimmed := strings.TrimSpace(agent.StripTransientUserBlocks(content))
	if trimmed == planApprovedMessage {
		return true
	}
	for _, prefix := range syntheticPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// syntheticPrefixes must be kept in sync with the synthetic user messages
// injected by the controller (planApprovedMessage, goal loop turns), agent loop
// (streamRecoveryMessage, finalReadinessRetryMessage, emptyFinalRetryMessage,
// executorHandoffRetryMessage in internal/agent/agent.go), and compaction
// folds (internal/agent/compact.go), which store summaries as user-role
// messages the chat UI must never render as user bubbles (#3653).
var syntheticPrefixes = []string{
	"Plan approved — plan mode is off",
	"Host final-answer readiness check failed",
	"You are already in the executor phase",
	"The previous assistant response was interrupted while a tool call",
	"The previous assistant response was interrupted during streaming",
	"The previous assistant response was interrupted before visible",
	"The previous assistant response finished without any visible answer",
	"<compaction-summary>",
	"Summary of the later conversation (compacted from here on):",
	"Summary of earlier conversation (compacted up to here):",
	"Continue pursuing the active goal",
	"The agent signaled goal completion and all tasks are marked done.",
	"Goal signaled complete but issues remain:",
	"No tool calls in recent turns.",
}

// Compose applies the plan-mode marker to a turn's text when plan mode is on,
// returning the message to actually send to the model. The frontend keeps
// showing the raw text as the user bubble.
func (c *Controller) Compose(text string) string {
	return c.compose(text, text, true)
}

func (c *Controller) compose(text, source string, includeHookContext bool) string {
	c.mu.Lock()
	plan := c.planMode
	responseLanguage := c.responseLanguage
	reasoningLanguage := c.reasoningLanguage
	c.mu.Unlock()
	notes := c.memory.drainPending()
	goal, goalStatus, goalResearchMode, autoResearchTaskID := c.goals.snapshot()

	if strings.TrimSpace(goal) != "" && goalStatus == GoalStatusRunning {
		prefix := activeGoalBlock(goal, goalResearchMode)
		if runtime := c.autoResearchRuntimeBlock(autoResearchTaskID); runtime != "" {
			prefix += "\n\n" + runtime
		}
		text = prefix + "\n\n" + text
	}
	if plan {
		text = PlanModeMarker + "\n\n" + text
	}
	text = agent.WithResponseLanguage(text, responseLanguage)
	text = agent.WithReasoningLanguageForSource(text, reasoningLanguage, source)

	// Memory added mid-session rides the turn (never the cached system prefix),
	// so it takes effect now without invalidating the prompt cache. It folds into
	// the system prefix on the next session, where it costs nothing per turn.
	if len(notes) > 0 {
		var b strings.Builder
		b.WriteString("<memory-update>\n")
		b.WriteString("The following project-memory changes were just made and apply from now on:\n")
		for _, n := range notes {
			b.WriteString("- " + n + "\n")
		}
		b.WriteString("</memory-update>\n\n")
		text = b.String() + text
	}

	// Background jobs that finished since the last turn ride the turn too, so the
	// model learns of completions even though the user-facing notices don't reach
	// its context. Like memory, this never touches the cache-stable prefix.
	if c.jobs != nil {
		if note := c.jobs.DrainCompletedNoteForSession(c.parentSessionID()); note != "" {
			text = "<background-jobs>\n" + note + "\n</background-jobs>\n\n" + text
		}
	}
	if includeHookContext {
		if block := c.drainHookContextBlock(); block != "" {
			text = block + "\n\n" + text
		}
	}
	return text
}

func (c *Controller) enqueueHookContexts(contexts []string) {
	if len(contexts) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, context := range contexts {
		context = strings.TrimSpace(context)
		if context == "" {
			continue
		}
		c.hookContexts = append(c.hookContexts, context)
	}
}

func (c *Controller) drainHookContextBlock() string {
	c.mu.Lock()
	contexts := c.hookContexts
	c.hookContexts = nil
	c.mu.Unlock()
	if len(contexts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<hook-context event="SessionStart">`)
	b.WriteString("\n")
	total := 0
	for i, context := range contexts {
		text, truncated := clipHookContext(context, maxHookContextChars)
		remaining := maxTotalHookContextChars - total
		if remaining <= 0 {
			fmt.Fprintf(&b, "[truncated: omitted %d additional hook context item(s)]\n", len(contexts)-i)
			break
		}
		text, totalTruncated := clipHookContext(text, remaining)
		total += len([]rune(text))
		if i > 0 {
			b.WriteString("\n---\n")
		}
		b.WriteString(escapeHookContext(text))
		b.WriteString("\n")
		if truncated || totalTruncated {
			b.WriteString("[truncated]\n")
		}
	}
	b.WriteString(`</hook-context>`)
	return b.String()
}

func clipHookContext(s string, max int) (string, bool) {
	r := []rune(s)
	if len(r) <= max {
		return s, false
	}
	if max < 0 {
		max = 0
	}
	return string(r[:max]), true
}

func escapeHookContext(s string) string {
	return strings.ReplaceAll(s, "</"+hookContextTag+">", "<\\/"+hookContextTag+">")
}

func (c *Controller) autoResearchRuntimeBlock(taskID string) string {
	if c.autoResearch == nil || strings.TrimSpace(taskID) == "" {
		return ""
	}
	summary, err := c.autoResearch.Summary(taskID)
	if err != nil {
		return "<autoresearch-runtime>\nstatus: invalid\nerror: " + strings.ReplaceAll(err.Error(), autoResearchRuntimeClose, "<\\/autoresearch-runtime>") + "\n</autoresearch-runtime>"
	}
	var b strings.Builder
	b.WriteString("<autoresearch-runtime>\n")
	b.WriteString("task_id: " + summary.TaskID + "\n")
	b.WriteString("status: " + summary.Status + "\n")
	b.WriteString("iteration: ")
	b.WriteString(strconv.Itoa(summary.Iteration))
	b.WriteString("\n")
	b.WriteString("current_direction: " + summary.CurrentDirection + "\n")
	b.WriteString("stale_count: ")
	b.WriteString(strconv.Itoa(summary.StaleCount))
	b.WriteString("\n")
	b.WriteString("pivot_count: ")
	b.WriteString(strconv.Itoa(summary.PivotCount))
	b.WriteString("\n")
	if summary.PivotRequired {
		b.WriteString("pivot_required: true\n")
	} else {
		b.WriteString("pivot_required: false\n")
	}
	b.WriteString("open_success_criteria: ")
	b.WriteString(strconv.Itoa(len(summary.OpenCriteria)))
	b.WriteString("\n")
	for _, criterion := range summary.OpenCriteria {
		b.WriteString("- ")
		b.WriteString(criterion.ID)
		b.WriteString(": ")
		b.WriteString(strings.ReplaceAll(criterion.Description, "\n", " "))
		b.WriteString("\n")
	}
	if summary.Blocker != "" {
		b.WriteString("blocker: " + summary.Blocker + "\n")
	}
	b.WriteString("next_required_action: " + summary.NextRequiredAction + "\n")
	b.WriteString("</autoresearch-runtime>")
	return b.String()
}

const autoResearchRuntimeClose = "</autoresearch-runtime>"

func reasoningLanguageBlock(lang string) string {
	return agent.ReasoningLanguageBlock(lang)
}

func (c *Controller) ComposeSynthetic(text string) string {
	c.mu.Lock()
	responseLang := c.responseLanguage
	lang := c.reasoningLanguage
	c.mu.Unlock()
	text = agent.WithResponseLanguage(text, responseLang)
	return agent.WithReasoningLanguageForSource(text, lang, text)
}

func activeGoalBlock(goal string, researchMode GoalResearchMode) string {
	goal = strings.TrimSpace(goal)
	goal = strings.ReplaceAll(goal, activeGoalClose, "<\\/active-goal>")
	var b strings.Builder
	b.WriteString(activeGoalOpen)
	b.WriteString("\n")
	b.WriteString(goal)
	b.WriteString("\n\n")
	b.WriteString(goalTaskContractInstructions)
	if shouldUseAutoResearch(goal, researchMode) {
		b.WriteString("\n\n")
		b.WriteString(autoResearchGoalInstructions)
	}
	b.WriteString("\n")
	b.WriteString(activeGoalClose)
	return b.String()
}

const goalTaskContractInstructions = `Goal mode: pursue this goal autonomously. Treat the user's goal as a task contract:
- Honor Context, Request, Output format, Constraints, and Checkpoint/Pause policy sections when present; otherwise infer a lightweight contract from the conversation and workspace.
- Preserve scope and output format. Do not invent requirements or hide uncertainty; state assumptions when sensible defaults are enough to proceed.
- Pause only when the next step involves an irreversible or externally visible operation, the requested scope has changed, or progress requires information only the user can provide. Otherwise keep working and report assumptions at the end.
- Complete only when the concrete request is done, the output format and constraints are satisfied, and relevant verification was attempted or reported unavailable.

Do not stop after describing a plan; execute the next useful step. End every goal-mode assistant reply with exactly one status marker on its own line: [goal:continue], [goal:complete], or [goal:blocked:<short reason>].`

const autoResearchGoalInstructions = `AutoResearch protocol: this goal looks like long-horizon research, debugging, optimization, or implementation work. Treat AutoResearch as a durable strategy for this Goal, not as a background daemon or a global skill.
- Say briefly in the first visible reply that the goal is being handled with AutoResearch and that host-owned state lives under .reasonix/autoresearch/<task-id>/, using the actual task_id from <autoresearch-runtime>.
- Keep dynamic state out of REASONIX.md, AGENTS.md, project memory, system prompts, and tool schemas. Use project-local .reasonix/autoresearch/ state only.
- Use the task_id and open_success_criteria in <autoresearch-runtime> as authoritative. The host creates task ids and owns state/task_spec.json, state/progress.json, state/findings.jsonl, state/directions_tried.json, state/iteration_log.jsonl, and logs/heartbeat.jsonl.
- Do not hand-edit the host-owned AutoResearch state files. When you have direct evidence for an open criterion, include an <autoresearch-evidence> block in your assistant reply so the host can persist it:
<autoresearch-evidence>
{"criterion_id":"objective_evidence","kind":"file","summary":"What was directly observed","source":"file","paths":["relative/path"],"accepted":true}
</autoresearch-evidence>
- Before each iteration, use the runtime summary as authoritative, choose a direction that differs materially from directions already tried, execute the smallest evidence-producing chunk, verify it, and report accepted evidence with <autoresearch-evidence> blocks.
- Increment stale_count when an iteration lacks accepted evidence or repeats a prior direction. At stale_count >= 2, make a structural pivot such as changing evidence source, entrypoint, implementation boundary, test oracle, benchmark, decomposition, environment, platform, or refutation angle. At stale_count >= 4, stop autonomous digging and ask for the smallest external input needed.
- Workers or subagents may gather evidence, but the orchestrator owns canonical state writes. Workers must not publish, push, delete, contact external systems, or write canonical state unless explicitly designated.
- Complete only after auditing every open success criterion in <autoresearch-runtime> against direct evidence. Public publishing, destructive changes, credential use, payments, external notifications, privacy-sensitive output, and cache-sensitive changes still require the normal Reasonix gates.`

func shouldUseAutoResearch(goal string, mode GoalResearchMode) bool {
	switch mode {
	case GoalResearchOn:
		return true
	case GoalResearchOff:
		return false
	}
	return isAutoResearchGoal(goal)
}

func shouldAutoStartResearchGoal(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "!") {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, ".reasonix/autoresearch/") {
		return true
	}
	for _, phrase := range autoResearchAutoStartPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	categories := autoResearchPhaseCount(lower)
	switch {
	case strings.Contains(lower, "彻底") && categories >= 3:
		return true
	case strings.Contains(lower, "完整") && categories >= 3:
		return true
	case strings.Contains(lower, "长期") && categories >= 2 && containsAnyGoalKeyword(lower, []string{"实验", "验证", "修复", "排查", "优化"}):
		return true
	case strings.Contains(lower, "thoroughly") && categories >= 3:
		return true
	case strings.Contains(lower, "complete") && categories >= 3:
		return true
	}
	return false
}

func isAutoResearchGoal(goal string) bool {
	trimmed := strings.TrimSpace(goal)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, ".reasonix/autoresearch/") {
		return true
	}
	for _, kw := range autoResearchStrongKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return autoResearchPhaseCount(lower) >= 4
}

func autoResearchPhaseCount(lower string) int {
	categories := 0
	for _, group := range autoResearchPhaseKeywords {
		if containsAnyGoalKeyword(lower, group) {
			categories++
		}
	}
	return categories
}

var autoResearchAutoStartPhrases = []string{
	"直到根因",
	"根因明确",
	"多轮排查",
	"不要原地打转",
	"别原地打转",
	"完整做成方案",
	"完整方案并验证",
	"跑实验",
	"反复验证",
	"系统性研究",
	"持续研究",
	"持续排查",
	"持续推进",
	"长期跑",
	"until the root cause",
	"root cause is clear",
	"debug until",
	"do not spin",
	"don't spin",
	"keep researching",
	"long-horizon",
	"long horizon",
	"long-running",
}

var autoResearchStrongKeywords = []string{
	"持续",
	"长期",
	"彻底",
	"直到根因",
	"根因明确",
	"多轮",
	"不要原地打转",
	"别原地打转",
	"完整方案",
	"完整做成方案",
	"跑实验",
	"反复验证",
	"长期优化",
	"系统性研究",
	"持续研究",
	"持续排查",
	"持续推进",
	"长期跑",
	"long-horizon",
	"long horizon",
	"long-running",
	"keep researching",
	"keep working",
	"root cause",
	"until the root cause",
	"do not spin",
	"don't spin",
	"thoroughly",
	"systematically",
}

var autoResearchPhaseKeywords = [][]string{
	{"研究", "调研", "排查", "分析", "定位", "诊断", "research", "investigate", "diagnose", "analyze", "analysis"},
	{"实现", "修复", "改造", "开发", "重构", "implement", "build", "fix", "refactor"},
	{"验证", "测试", "复现", "联调", "benchmark", "verify", "validate", "test", "reproduce"},
	{"优化", "完善", "提升", "收敛", "optimize", "improve", "tune", "polish"},
	{"文档", "方案", "说明", "总结", "document", "docs", "writeup", "plan"},
	{"发布", "上线", "提交", "pull request", "publish", "ship", "deploy"},
}

func containsAnyGoalKeyword(s string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// MemoryQuickAddNote parses the "# <note>" memory shortcut. The space after
// "#" is intentional: "#7", "#issue", and "#标题" are ordinary user prompts,
// not memory writes. Multi-line input starting with "# " is NOT treated as a
// quick-add note — it is almost certainly a Markdown heading in a structured
// prompt (e.g. "# Context\n\n- file.go\n# Objective"). Only single-line input
// may be a quick-add note.
func MemoryQuickAddNote(input string) (note string, ok bool) {
	trimmed := strings.TrimSpace(input)
	if strings.Contains(trimmed, "\n") {
		return "", false
	}
	if strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "#\t") {
		return strings.TrimSpace(trimmed[1:]), true
	}
	return "", false
}

// RememberCommandNote parses the explicit "/remember <note>" memory command.
func RememberCommandNote(input string) (note string, ok bool) {
	trimmed := strings.TrimSpace(input)
	switch {
	case trimmed == "/remember":
		return "", true
	case strings.HasPrefix(trimmed, "/remember ") || strings.HasPrefix(trimmed, "/remember\t"):
		return strings.TrimSpace(trimmed[len("/remember"):]), true
	default:
		return "", false
	}
}

type GoalCommandAction int

const (
	GoalCommandStatus GoalCommandAction = iota + 1
	GoalCommandSet
	GoalCommandClear
)

type GoalCommand struct {
	Action       GoalCommandAction
	Text         string
	Strict       bool
	ResearchMode GoalResearchMode
}

func ParseGoalCommand(input string) (GoalCommand, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed != "/goal" && !strings.HasPrefix(trimmed, "/goal ") && !strings.HasPrefix(trimmed, "/goal\t") {
		return GoalCommand{}, false
	}
	args := strings.TrimSpace(trimmed[len("/goal"):])
	strict, researchMode, actionArgs := parseLeadingGoalFlags(args)

	switch strings.ToLower(actionArgs) {
	case "", "status":
		return GoalCommand{Action: GoalCommandStatus, Strict: strict, ResearchMode: researchMode}, true
	case "clear", "off", "stop", "done":
		return GoalCommand{Action: GoalCommandClear, Strict: strict, ResearchMode: researchMode}, true
	default:
		return GoalCommand{Action: GoalCommandSet, Text: actionArgs, Strict: strict, ResearchMode: researchMode}, true
	}
}

func parseLeadingGoalFlags(args string) (bool, GoalResearchMode, string) {
	strict := false
	mode := GoalResearchAuto
	rest := strings.TrimLeftFunc(args, unicode.IsSpace)
	for rest != "" {
		token, after := leadingGoalToken(rest)
		switch strings.ToLower(token) {
		case "--strict":
			strict = true
		case "--research", "--auto-research", "--deep":
			mode = GoalResearchOn
		case "--simple", "--no-research":
			mode = GoalResearchOff
		default:
			return strict, mode, strings.TrimSpace(rest)
		}
		rest = strings.TrimLeftFunc(after, unicode.IsSpace)
	}
	return strict, mode, ""
}

func leadingGoalToken(s string) (string, string) {
	for i, r := range s {
		if unicode.IsSpace(r) {
			return s[:i], s[i:]
		}
	}
	return s, ""
}

// CustomCommand resolves a "/name args…" line against the loaded custom slash
// commands, returning the rendered prompt to send (found=false when no command
// matches). It does not apply the plan-mode marker — call Compose for that.
func (c *Controller) CustomCommand(input string) (sent string, found bool) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", false
	}
	name := strings.TrimPrefix(fields[0], "/")
	for _, cmd := range c.Commands() {
		if cmd.Name == name {
			return cmd.Render(fields[1:]), true
		}
	}
	return "", false
}

// resolveSkillInvocation resolves a "/<name> args…" line to its live Skill and
// task text. Submit uses RunAs to choose inline main-loop execution or isolated
// subagent execution; RunSkill remains the compatibility renderer used by
// management/existence checks and callers that explicitly need the body.
func (c *Controller) resolveSkillInvocation(input string) (skill.Skill, string, bool) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return skill.Skill{}, "", false
	}
	name := strings.TrimPrefix(fields[0], "/")
	sk, ok := c.skills.bySlashName(name)
	if !ok {
		return skill.Skill{}, "", false
	}
	return sk, strings.Join(fields[1:], " "), true
}

// RunSkill resolves a "/<name> args…" line against the loaded skills and
// renders its body. Controller.Submit does not use this renderer for
// runAs=subagent skills: direct slash invocation executes those through the
// isolated SkillRunner instead.
func (c *Controller) RunSkill(input string) (sent string, found bool) {
	sk, task, ok := c.resolveSkillInvocation(input)
	if !ok {
		return "", false
	}
	return skill.Render(sk, task), true
}

// MCPPrompt resolves a "/mcp__server__prompt args…" line: it maps the positional
// args onto the prompt's declared arguments and fetches the rendered prompt from
// the MCP server (an async prompts/get). found is false when no such prompt
// exists; err carries a fetch failure. Honours ctx.
func (c *Controller) MCPPrompt(ctx context.Context, input string) (sent string, found bool, err error) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", false, nil
	}
	name := strings.TrimPrefix(fields[0], "/")

	prompts := c.mcp.prompts()
	idx := -1
	for i := range prompts {
		if prompts[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", false, nil
	}

	args := map[string]string{}
	for i, a := range prompts[idx].Args {
		if i+1 < len(fields) {
			args[a.Name] = fields[i+1]
		}
	}
	text, err := prompts[idx].Get(ctx, args)
	if err != nil {
		return "", true, err
	}
	return text, true, nil
}
