package skill

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
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
	runner := func(_ context.Context, sk Skill, task string, _ SubagentRunOptions) (string, error) {
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

func TestRunSkillSubagentResolvesProfile(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/deep.md", "---\ndescription: deep\nrunAs: subagent\nmodel: deepseek-pro\neffort: max\n---\nbody")
	tl := NewRunSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}), nil)

	pr, ok := tl.(interface {
		ResolveProfile(json.RawMessage) *event.Profile
	})
	if !ok {
		t.Fatal("run_skill should expose ResolveProfile")
	}
	got := pr.ResolveProfile(json.RawMessage(`{"name":"deep","arguments":"x"}`))
	if got == nil || got.Model != "deepseek-pro" || got.Effort != "max" {
		t.Fatalf("profile = %+v, want deepseek-pro/max", got)
	}
}

func TestRunSkillSubagentRequiresArgs(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/dig.md", "---\ndescription: dig\nrunAs: subagent\n---\nbody")
	runner := func(_ context.Context, _ Skill, _ string, _ SubagentRunOptions) (string, error) {
		return "x", nil
	}
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
	runner := func(_ context.Context, sk Skill, task string, _ SubagentRunOptions) (string, error) {
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

func TestBuiltinSubagentToolsPassContinuationOptions(t *testing.T) {
	var got SubagentRunOptions
	runner := func(_ context.Context, _ Skill, _ string, opts SubagentRunOptions) (string, error) {
		got = opts
		return "ok", nil
	}
	tools := BuiltinSubagentTools(New(Options{HomeDir: t.TempDir()}), runner)
	var review interface {
		Name() string
		Execute(context.Context, json.RawMessage) (string, error)
	}
	for _, tl := range tools {
		if tl.Name() == "review" {
			review = tl
			break
		}
	}
	if review == nil {
		t.Fatal("review wrapper tool not built")
	}
	if _, err := review.Execute(context.Background(), json.RawMessage(`{"task":"again","continue_from":"sa_prev"}`)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got.ContinueFrom != "sa_prev" || got.ForkFrom != "" {
		t.Fatalf("continuation opts = %+v, want continue_from sa_prev", got)
	}
}

func TestBuiltinSubagentToolResolvesProfile(t *testing.T) {
	store := New(Options{HomeDir: t.TempDir()})
	tools := BuiltinSubagentTools(store, nil, func(sk Skill) *event.Profile {
		return &event.Profile{Model: sk.Name + "-model", Effort: "max"}
	})
	var review interface {
		ResolveProfile(json.RawMessage) *event.Profile
	}
	for _, tl := range tools {
		if tl.Name() == "review" {
			review = tl.(interface {
				ResolveProfile(json.RawMessage) *event.Profile
			})
			break
		}
	}
	if review == nil {
		t.Fatal("review tool not found")
	}
	got := review.ResolveProfile(json.RawMessage(`{"task":"general"}`))
	if got == nil || got.Model != "review-model" || got.Effort != "max" {
		t.Fatalf("profile = %+v, want review-model/max", got)
	}
}

func TestInstallSkill(t *testing.T) {
	home := t.TempDir()
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	tl := NewInstallSkillTool(st, nil)

	out, err := tl.Execute(context.Background(), json.RawMessage(
		`{"name":"deploy","description":"ship it","body":"steps","runAs":"subagent","model":"deepseek-pro","effort":"max","allowedTools":["bash","read_file"]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Errorf("expected ok result, got %s", out)
	}
	var res struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("result JSON: %v", err)
	}
	wantPath := filepath.Join(home, ".reasonix", "skills", "deploy", SkillFile)
	if res.Path != wantPath {
		t.Fatalf("install_skill should report canonical path %s, got %s", wantPath, res.Path)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("install_skill should write canonical SKILL.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".reasonix", "skills", "deploy.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("install_skill should not write legacy flat deploy.md, stat err=%v", err)
	}
	// Round-trips through the store with the frontmatter we wrote.
	sk, ok := st.Read("deploy")
	if !ok {
		t.Fatal("installed skill not readable")
	}
	if sk.RunAs != RunSubagent || sk.Model != "deepseek-pro" || sk.Effort != "max" || len(sk.AllowedTools) != 2 {
		t.Errorf("frontmatter not round-tripped: runAs=%s model=%q effort=%q tools=%v", sk.RunAs, sk.Model, sk.Effort, sk.AllowedTools)
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

func TestReadSkillLoadsInlineAndIsReadOnly(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/note.md", "---\ndescription: take a note\n---\nDo the thing.")
	tl := NewReadSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}))

	if !tl.ReadOnly() {
		t.Fatal("read_skill must be ReadOnly so it works in plan mode")
	}
	out, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"note","arguments":"with args"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "Do the thing.") || !strings.Contains(out, "Arguments: with args") {
		t.Errorf("inline body/args missing:\n%s", out)
	}
}

func TestReadSkillRejectsSubagent(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, home, ".reasonix/skills/dig.md", "---\ndescription: dig\nrunAs: subagent\n---\nbody")
	tl := NewReadSkillTool(New(Options{HomeDir: home, DisableBuiltins: true}))

	if _, err := tl.Execute(context.Background(), json.RawMessage(`{"name":"dig"}`)); err == nil || !strings.Contains(err.Error(), "run_skill") {
		t.Fatalf("read_skill on a subagent skill should point to run_skill, got %v", err)
	}
}
