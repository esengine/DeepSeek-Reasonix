package skill

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunSkillInline(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/note.md", "---\ndescription: take a note\n---\nDo the thing.")
	tl := NewRunSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), nil)

	out, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"note","arguments":"with args"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.HasPrefix(out, "<skill-pin name=\"note\">") || !strings.HasSuffix(out, "</skill-pin>") {
		t.Errorf("inline skill should be skill-pin wrapped:\n%s", out)
	}
	if !strings.Contains(out, "Do the thing.") || !strings.Contains(out, "Arguments: with args") {
		t.Errorf("body/args missing:\n%s", out)
	}
}

func TestRunSkillUnknown(t *testing.T) {
	tl := NewRunSkillTool(New(Options{HomeDir: t.TempDir(), DisableBuiltins: true}), nil)
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"nope"}`)); err == nil {
		t.Error("unknown skill should error")
	}
}

func TestRunSkillSubagentNeedsRunner(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/dig.md", "---\ndescription: dig\nrunAs: subagent\n---\nbody")
	tl := NewRunSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), nil) // nil runner
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"dig","arguments":"go"}`)); err == nil {
		t.Error("subagent skill with no runner should error, not silently inline")
	}
}

func TestRunSkillSubagentRuns(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/dig.md", "---\ndescription: dig\nrunAs: subagent\n---\nbody")
	var gotTask string
	runner := func(_ context.Context, sk Skill, task string) (string, error) {
		gotTask = task
		return "answer from " + sk.Name, nil
	}
	tl := NewRunSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), runner)
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"dig","arguments":"find X"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotTask != "find X" {
		t.Errorf("runner got task %q", gotTask)
	}
	if out != "answer from dig" {
		t.Errorf("runner output not returned: %q", out)
	}
}

func TestRunSkillSubagentRequiresArgs(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/dig.md", "---\ndescription: dig\nrunAs: subagent\n---\nbody")
	runner := func(_ context.Context, _ Skill, _ string) (string, error) { return "x", nil }
	tl := NewRunSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), runner)
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"dig"}`)); err == nil {
		t.Error("subagent skill should require arguments")
	}
}

func TestCleanSkillName(t *testing.T) {
	cases := map[string]string{
		"explore":              "explore",
		"explore [🧬 subagent]": "explore",
		"[🧬 subagent] explore": "explore",
		" review ":             "review",
		"[only a tag]":         "",
		"":                     "",
	}
	for in, want := range cases {
		if got := cleanSkillName(in); got != want {
			t.Errorf("cleanSkillName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuiltinSubagentToolsRunner(t *testing.T) {
	var ran string
	runner := func(_ context.Context, sk Skill, task string) (string, error) {
		ran = sk.Name + ":" + task
		return "ok", nil
	}
	tools := BuiltinSubagentTools(New(Options{HomeDir: t.TempDir()}), runner)
	var explore interface {
		Name() string
		Execute(context.Context, json.RawMessage) (string, error)
	}
	for _, tl := range tools {
		if tl.Name() == "explore" {
			explore = tl
		}
	}
	if explore == nil {
		t.Fatal("explore wrapper tool not built")
	}
	if _, err := explore.Execute(context.Background(), json.RawMessage(`{"task":"map the loop"}`)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if ran != "explore:map the loop" {
		t.Errorf("runner not invoked correctly: %q", ran)
	}
}

func TestInstallSkill(t *testing.T) {
	home := t.TempDir()
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	tl := NewInstallSkillTool(st, nil)

	out, err := tl.Execute(context.Background(), json.RawMessage(
		`{"name":"deploy","description":"ship it","body":"steps","runAs":"subagent","allowedTools":["bash","read_file"]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Errorf("expected ok result, got %s", out)
	}
	// Round-trips through the store with the frontmatter we wrote.
	sk, ok := st.Read("deploy")
	if !ok {
		t.Fatal("installed skill not readable")
	}
	if sk.RunAs != RunSubagent || len(sk.AllowedTools) != 2 {
		t.Errorf("frontmatter not round-tripped: runAs=%s tools=%v", sk.RunAs, sk.AllowedTools)
	}
	// Refuses overwrite.
	if _, err := tl.Execute(context.Background(), json.RawMessage(
		`{"name":"deploy","description":"again","body":"x"}`)); err == nil {
		t.Error("install_skill should refuse to overwrite")
	}
	// Requires description.
	if _, err := tl.Execute(context.Background(), json.RawMessage(
		`{"name":"x","description":"","body":"b"}`)); err == nil {
		t.Error("install_skill should require a description")
	}
}
