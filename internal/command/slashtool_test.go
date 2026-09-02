package command

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/testenv"
)

func runSlash(t *testing.T, tl interface {
	Execute(context.Context, json.RawMessage) (string, error)
}, args map[string]any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return tl.Execute(context.Background(), raw)
}

func sampleTool() interface {
	Execute(context.Context, json.RawMessage) (string, error)
	Name() string
	ReadOnly() bool
	Description() string
} {
	return NewSlashCommandTool([]SlashEntry{
		{Name: "review", Description: "review the diff", ArgHint: "[path]",
			Render: func(a []string) string { return "REVIEW " + strings.Join(a, ",") }},
		// Leading slash on Name should be tolerated.
		{Name: "/git:commit", Description: "commit",
			Render: func(a []string) string { return "COMMIT" }},
	}).(interface {
		Execute(context.Context, json.RawMessage) (string, error)
		Name() string
		ReadOnly() bool
		Description() string
	})
}

func TestSlashToolBasics(t *testing.T) {
	tl := sampleTool()
	if tl.Name() != "slash_command" {
		t.Errorf("name = %q", tl.Name())
	}
	if !tl.ReadOnly() {
		t.Error("slash_command should be read-only")
	}
	// The names are a result of the tool, not part of its schema: a per-project
	// list in a description diverges the cached prefix between projects.
	if strings.Contains(tl.Description(), "review") || strings.Contains(tl.Description(), "git:commit") {
		t.Errorf("a configured name reached the tool schema: %q", tl.Description())
	}
	listed, err := runSlash(t, tl, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed, "review") || !strings.Contains(listed, "git:commit") {
		t.Errorf("list should name what the description no longer does: %q", listed)
	}
}

func TestSlashToolExpandsWithArgs(t *testing.T) {
	tl := sampleTool()
	out, err := runSlash(t, tl, map[string]any{"command": "review", "arguments": "a b"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "REVIEW a,b") {
		t.Errorf("args not passed to Render: %q", out)
	}
	if !strings.Contains(out, "follow these instructions now") {
		t.Errorf("expansion should be framed as an instruction: %q", out)
	}
}

func TestSlashToolLeadingSlashAndName(t *testing.T) {
	tl := sampleTool()
	// Caller passes a leading slash; entry was also registered with one.
	out, err := runSlash(t, tl, map[string]any{"command": "/git:commit"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "COMMIT") {
		t.Errorf("leading-slash command not resolved: %q", out)
	}
}

func TestSlashToolList(t *testing.T) {
	tl := sampleTool()
	for _, cmd := range []string{"", "list", "LIST"} {
		out, err := runSlash(t, tl, map[string]any{"command": cmd})
		if err != nil {
			t.Fatalf("list(%q): %v", cmd, err)
		}
		if !strings.Contains(out, "/review") || !strings.Contains(out, "[path]") || !strings.Contains(out, "/git:commit") {
			t.Errorf("list(%q) missing entries: %q", cmd, out)
		}
	}
}

func TestSlashToolUnknown(t *testing.T) {
	tl := sampleTool()
	_, err := runSlash(t, tl, map[string]any{"command": "nope"})
	if err == nil {
		t.Fatal("unknown command should error")
	}
	if !strings.Contains(err.Error(), "review") {
		t.Errorf("error should list available commands: %v", err)
	}
}

func TestSlashToolEmptyRegistry(t *testing.T) {
	tl := NewSlashCommandTool(nil)
	out, err := tl.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No slash commands") {
		t.Errorf("empty list = %q", out)
	}
	// The description must not report the count either: that is the same
	// per-project fact, one bit wide.
	if tl.Description() != sampleTool().Description() {
		t.Errorf("description varies with what is configured: %q", tl.Description())
	}
}

func TestSlashToolNameClashCommandWins(t *testing.T) {
	// Skills added first, command second — command should win the name.
	tl := NewSlashCommandTool([]SlashEntry{
		{Name: "dup", Render: func([]string) string { return "FROM-SKILL" }},
		{Name: "dup", Render: func([]string) string { return "FROM-COMMAND" }},
	})
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"command":"dup"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "FROM-COMMAND") {
		t.Errorf("later entry should win the clash: %q", out)
	}
}

func TestPluginSlashToolShowsOnlyCanonicalQualifiedName(t *testing.T) {
	dir := testenv.TempDir(t)
	write(t, dir, "plan.md", "---\ndescription: Plan work\n---\nPlan $ARGUMENTS")
	plain, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := LoadRoots(Root{Path: dir, Plugin: "pwf"})
	if err != nil {
		t.Fatal(err)
	}
	entries := func(cmds []Command) []SlashEntry {
		out := make([]SlashEntry, 0, len(cmds))
		for _, cmd := range cmds {
			if cmd.Hidden {
				continue
			}

			out = append(out, SlashEntry{Name: cmd.Name, Description: cmd.Description, ArgHint: cmd.ArgHint, Render: func(args []string) string { return cmd.Render(args) }})
		}
		return out
	}
	plainTool := NewSlashCommandTool(entries(plain))
	ownedTool := NewSlashCommandTool(entries(owned))
	plainList, err := runSlash(t, plainTool, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plainList, "plan") {
		t.Fatalf("plain listing = %q", plainList)
	}
	ownedList, err := runSlash(t, ownedTool, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ownedList, "pwf:plan") {
		t.Fatalf("plugin listing should carry the canonical name, got %q", ownedList)
	}
	// Neither qualification may reach the schema — description included.
	if plainTool.Description() != ownedTool.Description() {
		t.Fatalf("plugin qualification changed the description: %q vs %q", plainTool.Description(), ownedTool.Description())
	}
	if string(plainTool.Schema()) != string(ownedTool.Schema()) {
		t.Fatal("plugin qualification must not change the slash_command schema")
	}
}
