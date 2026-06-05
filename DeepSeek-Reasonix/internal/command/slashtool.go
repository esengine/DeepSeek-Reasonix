package command

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/tool"
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
}

// slashCommandTool lets the model invoke a loaded slash command by name. Unlike a
// tool that performs an action and returns a result, a slash command is a *prompt
// template*: the tool returns the expanded prompt text, which the model then reads
// and acts on within the same turn — mirroring what typing "/name" does for a
// human. Calling with no name (or "list") returns the available commands.
type slashCommandTool struct {
	entries map[string]SlashEntry
	names   []string // sorted, for a stable listing
}

// NewSlashCommandTool builds the tool from the invocable entries (custom commands
// + skills, adapted by the caller). A later entry wins on a name clash, matching
// the prompt's command>skill precedence when the caller orders them that way.
func NewSlashCommandTool(entries []SlashEntry) tool.Tool {
	m := make(map[string]SlashEntry, len(entries))
	for _, e := range entries {
		name := strings.TrimPrefix(strings.TrimSpace(e.Name), "/")
		if name == "" {
			continue
		}
		e.Name = name
		m[name] = e
	}
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return &slashCommandTool{entries: m, names: names}
}

func (*slashCommandTool) Name() string { return "slash_command" }

func (*slashCommandTool) ReadOnly() bool { return true }

func (t *slashCommandTool) Description() string {
	var b strings.Builder
	b.WriteString("Invoke a project slash command (a reusable prompt template or skill) by name. " +
		"Returns the command's expanded prompt text for you to act on in this turn — it does not run on its own. " +
		"Call with an empty command (or \"list\") to see what's available. ")
	if len(t.names) == 0 {
		b.WriteString("No slash commands are configured in this project.")
		return b.String()
	}
	b.WriteString("Available: ")
	for i, n := range t.names {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(n)
	}
	b.WriteString(".")
	return b.String()
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
	e, ok := t.entries[name]
	if !ok {
		return "", fmt.Errorf("no slash command %q; available: %s", name, strings.Join(t.names, ", "))
	}
	args := strings.Fields(p.Arguments)
	expanded := e.Render(args)
	// Frame the expansion so the model treats it as an instruction to follow now,
	// not as data to echo back.
	return fmt.Sprintf("Expanded /%s — follow these instructions now:\n\n%s", name, expanded), nil
}

func (t *slashCommandTool) list() string {
	if len(t.names) == 0 {
		return "No slash commands are configured in this project."
	}
	var b strings.Builder
	b.WriteString("Available slash commands:\n")
	for _, n := range t.names {
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
