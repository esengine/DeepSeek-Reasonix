package control

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"reasonix/internal/ablation"
	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/memory"
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
	if trimmed := strings.TrimSpace(agent.StripTransientUserBlocks(content)); trimmed == planApprovedMessage {
		return true
	}
	// The prefix list lives in internal/agent (agent.SyntheticUserPrefixes) so
	// preview/title/turn-count derivations there share the exact same filter
	// (#3653).
	return agent.IsSyntheticUserText(content)
}

// Compose applies the plan-mode marker to a turn's text when plan mode is on,
// returning the message to actually send to the model. The frontend keeps
// showing the raw text as the user bubble.
func (c *Controller) Compose(text string) string {
	return c.compose(text, text, true)
}

func (c *Controller) compose(text, source string, includeHookContext bool) string {
	goal, goalStatus := c.goals.snapshot()
	return c.composeWithGoal(
		text,
		source,
		includeHookContext,
		goal,
		goalStatus,
	)
}

func (c *Controller) composeWithGoal(
	text, source string,
	includeHookContext bool,
	goal, goalStatus string,
) string {
	c.mu.Lock()
	plan := c.planMode
	responseLanguage := c.responseLanguage
	reasoningLanguage := c.reasoningLanguage
	c.mu.Unlock()
	notes := c.memory.drainPending()

	if strings.TrimSpace(goal) != "" && goalStatus == GoalStatusRunning {
		prefix := activeGoalBlock(goal)
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
		// Relevant facts ride only the real user-turn tail. This preserves the
		// stable system/tool prefix and keeps synthetic recovery turns free of
		// accidental recall. A just-written fact already arrives in memory-update.
		if len(notes) == 0 && !c.ablation.Off(ablation.Retrieval) {
			result := c.memory.recall(source)
			event.RecordMemoryRecall(c.sink, memoryRecallAudit(result))
			if block := result.Block(); block != "" {
				text = strings.TrimRight(text, "\n") + "\n\n" + block
			}
		} else if len(notes) > 0 {
			c.memory.recordRecall(memory.RecallResult{
				Query:      strings.TrimSpace(source),
				Suppressed: "memory update already supplies the new fact",
			})
		}
	}
	return text
}

// LastMemoryRecall returns the last real turn's automatic-recall decision for
// diagnostics and context-management surfaces.
func (c *Controller) LastMemoryRecall() memory.RecallResult {
	return c.memory.lastRecallResult()
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

func activeGoalBlock(goal string) string {
	goal = strings.TrimSpace(goal)
	goal = strings.ReplaceAll(goal, activeGoalClose, "<\\/active-goal>")
	var b strings.Builder
	b.WriteString(activeGoalOpen)
	b.WriteString("\n")
	b.WriteString(goal)
	b.WriteString("\n\n")
	b.WriteString(goalTaskContractInstructions)
	b.WriteString("\n")
	b.WriteString(activeGoalClose)
	return b.String()
}

const goalTaskContractInstructions = `Goal mode: pursue this goal autonomously. Treat the user's goal as a task contract:
- Honor Context, Request, Output format, Constraints, and Checkpoint/Pause policy sections when present; otherwise infer a lightweight contract from the conversation and workspace.
- Preserve scope and output format. Do not invent requirements or hide uncertainty; state assumptions when sensible defaults are enough to proceed.
- Pause only when the next step involves an irreversible or externally visible operation, the requested scope has changed, or progress requires information only the user can provide. Otherwise keep working and report assumptions at the end.
- Complete only when the concrete request is done, the output format and constraints are satisfied, and relevant verification was attempted or reported unavailable.

Do not stop after describing a plan; execute the next useful step. End every goal-mode turn by calling the update_goal tool with your disposition: continue (work is ongoing — give the next concrete step in next_action), complete (only when fully done and verified), or blocked (only when the user can unblock). The host validates your claim and decides whether to continue automatically.`

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
	GoalCommandPause
	GoalCommandResume
)

type GoalCommand struct {
	Action               GoalCommandAction
	Text                 string
	Strict               bool
	ResearchMode         GoalResearchMode
	DeprecatedBudgetFlag bool
}

const GoalBudgetFlagDeprecatedNotice = "This /goal budget flag is deprecated and no longer changes the execution limit; Goal now runs continuously by default."

func ParseGoalCommand(input string) (GoalCommand, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed != "/goal" && !strings.HasPrefix(trimmed, "/goal ") && !strings.HasPrefix(trimmed, "/goal\t") {
		return GoalCommand{}, false
	}
	args := strings.TrimSpace(trimmed[len("/goal"):])
	strict, researchMode, actionArgs := parseLeadingGoalFlags(args)
	deprecatedBudgetFlag := researchMode != GoalResearchAuto

	switch strings.ToLower(actionArgs) {
	case "", "status":
		return GoalCommand{Action: GoalCommandStatus, Strict: strict, ResearchMode: researchMode, DeprecatedBudgetFlag: deprecatedBudgetFlag}, true
	case "clear", "off", "stop", "done":
		return GoalCommand{Action: GoalCommandClear, Strict: strict, ResearchMode: researchMode, DeprecatedBudgetFlag: deprecatedBudgetFlag}, true
	case "pause":
		return GoalCommand{Action: GoalCommandPause, Strict: strict, ResearchMode: researchMode, DeprecatedBudgetFlag: deprecatedBudgetFlag}, true
	case "resume":
		return GoalCommand{Action: GoalCommandResume, Strict: strict, ResearchMode: researchMode, DeprecatedBudgetFlag: deprecatedBudgetFlag}, true
	default:
		return GoalCommand{Action: GoalCommandSet, Text: actionArgs, Strict: strict, ResearchMode: researchMode, DeprecatedBudgetFlag: deprecatedBudgetFlag}, true
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
	return c.skills.render(sk, task), true
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
