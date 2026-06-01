package control

import (
	"context"
	"strings"

	"reasonix/internal/skill"
)

// PlanModeMarker is prepended to every user turn while plan mode is on. It rides
// in the user message (not the system prompt or tools), so the cache-stable
// prompt prefix is left untouched and the toggle costs nothing in cache hits.
const PlanModeMarker = "[Plan mode — read-only. Explore the codebase first (read_file, ls, grep, glob, web_fetch, task are available; writers are refused by the harness), then present a LAYERED plan as your reply and stop — do not write files, edit, or run side-effecting bash. Structure the plan as a two-level markdown list so it becomes a layered task list: each PHASE is a top-level numbered list item (a coherent milestone, e.g. \"1. Add the config loader\"), and each phase's concrete, verifiable sub-steps are bullets indented beneath it (e.g. \"   - parse the TOML into Config\"). Use plain numbered list items for phases — do NOT write phases as markdown headings (##, ###) — so both levels parse. Keep phases few (about 2-6). The user will be asked to approve before any changes are made.]"

// Compose applies the plan-mode marker to a turn's text when plan mode is on,
// returning the message to actually send to the model. The frontend keeps
// showing the raw text as the user bubble.
func (c *Controller) Compose(text string) string {
	c.mu.Lock()
	plan := c.planMode
	notes := c.pendingMemory
	c.pendingMemory = nil
	c.mu.Unlock()

	if plan {
		text = PlanModeMarker + "\n\n" + text
	}

	// Memory added mid-session rides the turn (never the cached system prefix),
	// so it takes effect now without invalidating the prompt cache. It folds into
	// the system prefix on the next session, where it costs nothing per turn.
	if len(notes) > 0 {
		var b strings.Builder
		b.WriteString("<memory-update>\n")
		b.WriteString("The following was just saved to project memory and applies from now on:\n")
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
		if note := c.jobs.DrainCompletedNote(); note != "" {
			text = "<background-jobs>\n" + note + "\n</background-jobs>\n\n" + text
		}
	}
	return text
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
	for _, cmd := range c.commands {
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
	for _, sk := range c.skills {
		if sk.Name == name {
			return skill.Render(sk, strings.Join(fields[1:], " ")), true
		}
	}
	return "", false
}

// MCPPrompt resolves a "/mcp__server__prompt args…" line: it maps the positional
// args onto the prompt's declared arguments and fetches the rendered prompt from
// the MCP server (an async prompts/get). found is false when no such prompt
// exists; err carries a fetch failure. Honours ctx.
func (c *Controller) MCPPrompt(ctx context.Context, input string) (sent string, found bool, err error) {
	if c.host == nil {
		return "", false, nil
	}
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", false, nil
	}
	name := strings.TrimPrefix(fields[0], "/")

	prompts := c.host.Prompts()
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
