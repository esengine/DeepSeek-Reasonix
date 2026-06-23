package control

import (
	"context"
	"strings"
	"unicode"

	"reasonix/internal/agent"
	"reasonix/internal/skill"
)

// PlanModeMarker is prepended to every user turn while plan mode is on. It rides
// in the user message (not the system prompt or tools), so the cache-stable
// prompt prefix is left untouched and the toggle costs nothing in cache hits.
const PlanModeMarker = "[Plan mode — read-only. Explore the codebase first (read_file, ls, grep, glob, web_fetch, task, ask are available; writers are refused by the harness). Before planning, if a decision that is genuinely the user's — tech stack, an ambiguous requirement, scope, an irreversible choice — would materially shape the plan and you can't settle it from the codebase or a sensible default, use the ask tool to clarify it first; otherwise pick the obvious default and state the assumption in the plan instead of asking. Then present a LAYERED plan as your reply and stop — do not write files, edit, or run side-effecting bash. Structure the plan as a two-level markdown list so it becomes a layered task list: each PHASE is a top-level numbered list item (a coherent milestone, e.g. \"1. Add the config loader\"), and each phase's concrete, verifiable sub-steps are bullets indented beneath it (e.g. \"   - parse the TOML into Config\"). Use plain numbered list items for phases — do NOT write phases as markdown headings (##, ###) — so both levels parse. Keep phases few (about 2-6). The user will be asked to approve before any changes are made.]"

const (
	activeGoalOpen  = "<active-goal>"
	activeGoalClose = "</active-goal>"
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
	s = strings.TrimPrefix(s, PlanModeMarker+"\n\n")
	s = strings.TrimPrefix(s, PlanModeMarker)
	s = strings.TrimSpace(s)
	return s
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
	"Continue pursuing the active goal.",
	"The agent signaled goal completion and all tasks are marked done.",
	"Goal signaled complete but issues remain:",
	"No tool calls in recent turns.",
}

// Compose applies the plan-mode marker to a turn's text when plan mode is on,
// returning the message to actually send to the model. The frontend keeps
// showing the raw text as the user bubble.
func (c *Controller) Compose(text string) string {
	c.mu.Lock()
	plan := c.planMode
	reasoningLanguage := c.reasoningLanguage
	c.mu.Unlock()
	notes := c.memory.drainPending()
	goal, goalStatus, goalResearchMode := c.goals.snapshot()

	if strings.TrimSpace(goal) != "" && goalStatus == GoalStatusRunning {
		text = activeGoalBlock(goal, goalResearchMode) + "\n\n" + text
	}
	if plan {
		text = PlanModeMarker + "\n\n" + text
	}
	text = agent.WithReasoningLanguage(text, reasoningLanguage)

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
	return text
}

func reasoningLanguageBlock(lang string) string {
	return agent.ReasoningLanguageBlock(lang)
}

func (c *Controller) ComposeSynthetic(text string) string {
	c.mu.Lock()
	lang := c.reasoningLanguage
	c.mu.Unlock()
	return agent.WithReasoningLanguage(text, lang)
}

func activeGoalBlock(goal string, researchMode GoalResearchMode) string {
	goal = strings.TrimSpace(goal)
	goal = strings.ReplaceAll(goal, activeGoalClose, "<\\/active-goal>")
	var b strings.Builder
	b.WriteString(activeGoalOpen)
	b.WriteString("\n")
	b.WriteString(goal)
	b.WriteString("\n\n")
	b.WriteString("Goal mode: pursue this goal autonomously. Keep working across turns until the goal is complete. Prefer sensible defaults over asking the user; use ask only when you are truly blocked on a user-owned decision. Do not stop after describing a plan; execute the next useful step. End every goal-mode assistant reply with exactly one status marker on its own line: [goal:continue], [goal:complete], or [goal:blocked:<short reason>].")
	if shouldUseAutoResearch(goal, researchMode) {
		b.WriteString("\n\n")
		b.WriteString(autoResearchGoalInstructions)
	}
	b.WriteString("\n")
	b.WriteString(activeGoalClose)
	return b.String()
}

const autoResearchGoalInstructions = `AutoResearch protocol: this goal looks like long-horizon research, debugging, optimization, or implementation work. Treat AutoResearch as a durable strategy for this Goal, not as a background daemon or a global skill.
- Say briefly in the first visible reply that the goal is being handled with AutoResearch and that state will live under .reasonix/autoresearch/<task-id>/.
- Keep dynamic state out of REASONIX.md, AGENTS.md, project memory, system prompts, and tool schemas. Use project-local .reasonix/autoresearch/ state only.
- For a new task, create a collision-resistant task id YYYYMMDD-HHMMSS-slug, check .reasonix/autoresearch/ first, and append -2, -3, etc. only on collision. Reuse an explicitly supplied .reasonix/autoresearch/<task-id>/ path exactly.
- Maintain state/task_spec.md, state/progress.json, state/findings.jsonl, state/directions_tried.json, state/iteration_log.jsonl, and logs/heartbeat.jsonl. Record goal, scope, non-goals, allowed operations, success criteria, verification gates, iteration direction, evidence, stale_count, pivots, blockers, and completion summary.
- Before each iteration, read the existing state files as authoritative, append a heartbeat, choose a direction that differs materially from directions already tried, execute the smallest evidence-producing chunk, verify it, then persist JSON/JSONL state before reporting.
- Increment stale_count when an iteration lacks accepted evidence or repeats a prior direction. At stale_count >= 2, make a structural pivot such as changing evidence source, entrypoint, implementation boundary, test oracle, benchmark, decomposition, environment, platform, or refutation angle. At stale_count >= 4, stop autonomous digging and ask for the smallest external input needed.
- Workers or subagents may gather evidence, but the orchestrator owns canonical state writes. Workers must not publish, push, delete, contact external systems, or write canonical state unless explicitly designated.
- Complete only after auditing every success criterion in task_spec.md against direct evidence. Public publishing, destructive changes, credential use, payments, external notifications, privacy-sensitive output, and cache-sensitive changes still require the normal Reasonix gates.`

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

// RunSkill resolves a "/<name> args…" line against the loaded skills, returning
// the skill's rendered body to send as a turn (found=false when no skill
// matches). Invoking a skill by slash always inlines its body — the model reads
// and follows the playbook in the main loop; a subagent skill's isolation is
// only engaged when the model calls it via run_skill / the dedicated tool. The
// caller applies Compose for plan-mode/memory framing.
func (c *Controller) RunSkill(input string) (sent string, found bool) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", false
	}
	name := strings.TrimPrefix(fields[0], "/")
	if sk, ok := c.skillByName(name); ok {
		return skill.Render(sk, strings.Join(fields[1:], " ")), true
	}
	return "", false
}

func (c *Controller) skillByName(name string) (skill.Skill, bool) {
	if c.skillStore != nil {
		return c.skillStore.Read(name)
	}
	for _, sk := range c.skills {
		if sk.Name == name {
			return sk, true
		}
	}
	return skill.Skill{}, false
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
