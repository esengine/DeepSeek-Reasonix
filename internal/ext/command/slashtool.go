package command

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"reasonix/internal/contract/tool"
)

// SlashEntry is one invocable slash command exposed to the model through the
// slash_command tool. It is a uniform view over the two kinds the user can also
// type at the prompt — custom commands and skills — so the tool need not know
// which is which. Render turns positional args into the prompt text the command
// expands to (the same text typing "/name args" would send).
type SlashEntry struct {
	Name        string // without the leading slash, e.g. "review" or "git:commit"
	Description string
	ArgHint     string                     // optional argument hint, for the listing
	Render      func(args []string) string // expands the template/playbook with args
	// Skill marks an entry the gate judges at call time; commands pass as is.
	Skill bool
	// Unlisted keeps an entry out of the listing the model reads without
	// refusing a call that names it. Nil lists the entry.
	Unlisted func() bool
}

// slashCommandTool lets the model invoke a loaded slash command by name. Unlike a
// tool that performs an action and returns a result, a slash command is a *prompt
// template*: the tool returns the expanded prompt text, which the model then reads
// and acts on within the same turn — mirroring what typing "/name" does for a
// human. Calling with no name (or "list") returns the available commands.
type slashCommandTool struct {
	gate    func() func(name string) error // one snapshot per call; nil admits everything
	entries map[string]SlashEntry
	names   []string // sorted, for a stable listing
}

// NewSlashCommandTool builds the tool from the invocable entries (custom commands
// + skills, adapted by the caller). A later entry wins on a name clash, matching
// the prompt's command>skill precedence when the caller orders them that way.
func NewSlashCommandTool(entries []SlashEntry, gate func() func(name string) error) tool.Tool {
	m := make(map[string]SlashEntry, len(entries))
	for _, e := range entries {
		name := strings.TrimPrefix(strings.TrimSpace(e.Name), "/")
		if name == "" {
			continue
		}
		e.Name = name
		m[name] = e
	}
	names := slices.Sorted(maps.Keys(m))
	return &slashCommandTool{gate: gate, entries: m, names: names}
}

func (*slashCommandTool) Name() string { return "slash_command" }

func (*slashCommandTool) ReadOnly() bool { return true }

// Description is constant. It used to enumerate the configured names, which put
// a per-project catalog inside a tool schema: it diverged the cached prefix
// between projects, and a reload rewrote it under a live session. The names are
// a result of the tool, not part of its contract — "list" returns them.
func (*slashCommandTool) Description() string {
	return "Invoke a project slash command (a reusable prompt template or skill) by name. " +
		"Returns the command's expanded prompt text for you to act on in this turn — it does not run on its own. " +
		"Call with an empty command (or \"list\") to see what is available."
}

func (*slashCommandTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "Slash command name (with or without a leading slash). Empty or \"list\" returns the available commands."},
			"arguments": {"type": "string", "description": "Arguments passed to the command, as you'd type them after the name (space-separated)."}
		}
	}`)
}

func (t *slashCommandTool) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Command   string `json:"command"`
		Arguments string `json:"arguments"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
	}
	name := strings.TrimPrefix(strings.TrimSpace(p.Command), "/")
	if name == "" || strings.EqualFold(name, "list") {
		return t.list(), nil
	}
	allowed := t.judge()
	e, ok := t.entries[name]
	if !ok {
		return "", fmt.Errorf("no slash command %q; available: %s", name, strings.Join(t.allowedNames(allowed), ", "))
	}
	if e.Skill && allowed != nil {
		if err := allowed(name); err != nil {
			return "", fmt.Errorf("slash_command: %w", err)
		}
	}
	args := strings.Fields(p.Arguments)
	expanded := e.Render(args)
	if e.Skill && expanded == "" {
		return "", fmt.Errorf("slash_command: /%s changed while it was being expanded; call it again", name)
	}
	// Frame the expansion so the model treats it as an instruction to follow now,
	// not as data to echo back.
	return fmt.Sprintf("Expanded /%s — follow these instructions now:\n\n%s", name, expanded), nil
}

func (t *slashCommandTool) judge() func(string) error {
	if t.gate == nil {
		return nil
	}
	return t.gate()
}

func (t *slashCommandTool) allowedNames(allowed func(string) error) []string {
	out := make([]string, 0, len(t.names))
	for _, n := range t.names {
		e := t.entries[n]
		if e.Unlisted != nil && e.Unlisted() {
			continue
		}
		if allowed == nil || !e.Skill || allowed(n) == nil {
			out = append(out, n)
		}
	}
	return out
}

func (t *slashCommandTool) list() string {
	names := t.allowedNames(t.judge())
	if len(names) == 0 {
		return "No slash commands are configured in this project."
	}
	var b strings.Builder
	b.WriteString("Available slash commands:\n")
	for _, n := range names {
		e := t.entries[n]
		line := "- /" + n
		if e.ArgHint != "" {
			line += " " + e.ArgHint
		}
		if e.Description != "" {
			line += " — " + e.Description
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
