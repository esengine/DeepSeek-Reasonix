package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/command"
	"reasonix/internal/hook"
	"reasonix/internal/memory"
	"reasonix/internal/outputstyle"
	"reasonix/internal/plugin"
	"reasonix/internal/skill"
)

func TestRenderSkillListUsesSharedVisualLanguage(t *testing.T) {
	width := 72
	got := renderSkillList(width, []skill.Skill{
		{Name: "explore", Description: strings.Repeat("long ", 30), Scope: skill.ScopeProject},
		{Name: "deep", Description: "run in isolation", Scope: skill.ScopeGlobal, RunAs: skill.RunSubagent},
	}, map[string]bool{"explore": true})
	for _, want := range []string{"skills (2)", "/explore", "(project)", "…", "/deep", "subagent", "disabled", "invoke:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("skill list missing %q:\n%s", want, got)
		}
	}
	assertLinesWithin(t, got, width)
}

func TestRenderSkillShowCapsLongBody(t *testing.T) {
	width := 80
	body := strings.Repeat("line\n", skillShowMaxLines+3)
	got := renderSkillShow(width, skill.Skill{
		Name:        "review",
		Description: "review code",
		Scope:       skill.ScopeBuiltin,
		Path:        "/very/long/path/to/SKILL.md",
		Body:        body,
	}, true)
	for _, want := range []string{"skill:", "review", "builtin", "disabled", "review code", "SKILL.md", "+3 more lines"} {
		if !strings.Contains(got, want) {
			t.Fatalf("skill show missing %q:\n%s", want, got)
		}
	}
	assertLinesWithin(t, got, width)
}

func TestViewProtectLinesCompactsLongBodyLines(t *testing.T) {
	got := viewProtectLines(strings.Repeat("x", 80)+"\nshort", 20)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "…") || visibleWidth(lines[0]) > 20 || lines[1] != "short" {
		t.Fatalf("protected lines = %q", got)
	}
}

func TestRenderMemoryGroupsDocsAndStore(t *testing.T) {
	width := 72
	store := memory.Store{Dir: filepath.Join(t.TempDir(), "memory")}
	if _, err := store.Save(memory.Memory{Name: "saved-fact", Title: "Saved Fact", Description: "remembered fact"}); err != nil {
		t.Fatalf("save memory: %v", err)
	}
	got := renderMemory(width, &memory.Set{
		Docs:  []memory.Source{{Path: "/Users/me/project/REASONIX.md", Scope: memory.ScopeProject}},
		Store: store,
		Index: store.Index(),
	})
	for _, want := range []string{"memory", "docs", "(project)", "REASONIX.md", "saved memories", "saved-fact", "Saved Fact", "doc edits apply next session"} {
		if !strings.Contains(got, want) {
			t.Fatalf("memory view missing %q:\n%s", want, got)
		}
	}
	assertLinesWithin(t, got, width)
}

func TestRenderOutputStylesUsesActiveStatus(t *testing.T) {
	width := 72
	got := renderOutputStyles(width, []outputstyle.OutputStyle{
		{Name: "concise", Description: "short answers", Builtin: true},
		{Name: "team", Description: strings.Repeat("custom ", 20), Builtin: false},
	}, "team")
	for _, want := range []string{"output styles", "concise", "(builtin)", "team", "(custom)", "active", "…"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output style view missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "*") {
		t.Fatalf("output style view should use active status instead of star:\n%s", got)
	}
	assertLinesWithin(t, got, width)
}

func TestRenderModelsUsesActiveStatus(t *testing.T) {
	width := 72
	got := renderModels(width, []string{"deepseek/v4", "openai/really-long-model-name-" + strings.Repeat("x", 80)}, "deepseek/v4")
	for _, want := range []string{"models", "deepseek/v4", "active", "…", "switch with /model"} {
		if !strings.Contains(got, want) {
			t.Fatalf("model view missing %q:\n%s", want, got)
		}
	}
	assertLinesWithin(t, got, width)
}

func TestRenderHooksUsesSharedVisualLanguage(t *testing.T) {
	width := 72
	got := renderHooks(width, []hook.ResolvedHook{{
		HookConfig: hook.HookConfig{Command: strings.Repeat("echo ", 30)},
		Event:      hook.PreToolUse,
		Scope:      hook.ScopeProject,
	}})
	for _, want := range []string{"hooks (1 active)", "PreToolUse", "project", "…", ".reasonix/settings.json"} {
		if !strings.Contains(got, want) {
			t.Fatalf("hooks view missing %q:\n%s", want, got)
		}
	}
	assertLinesWithin(t, got, width)
}

func TestRenderHooksShowsPermissionRequestMatch(t *testing.T) {
	width := 72
	got := renderHooks(width, []hook.ResolvedHook{{
		HookConfig: hook.HookConfig{Command: "notify", Match: "bash"},
		Event:      hook.PermissionRequest,
		Scope:      hook.ScopeGlobal,
	}})
	for _, want := range []string{"PermissionRequest", "global", "bash", "notify"} {
		if !strings.Contains(got, want) {
			t.Fatalf("hooks view missing %q:\n%s", want, got)
		}
	}
	assertLinesWithin(t, got, width)
}

func TestRenderHelpGroupsCommands(t *testing.T) {
	width := 72
	got := renderHelp(width,
		[]command.Command{{Name: "review", Description: "review code"}},
		[]skill.Skill{{Name: "explore", Description: "inspect repo"}},
		[]plugin.Prompt{{Name: "mcp__docs__summarize", Description: "summarize docs"}},
	)
	for _, want := range []string{"commands", "built-in", "/tree", "custom", "/review", "skills", "/explore", "MCP prompts", "/mcp__docs__summarize"} {
		if !strings.Contains(got, want) {
			t.Fatalf("help view missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, " · ") {
		t.Fatalf("help view should not use one-line command chains:\n%s", got)
	}
	assertLinesWithin(t, got, width)
}

func TestRenderSkillPathsStaysWithinWidth(t *testing.T) {
	width := 72
	got := renderSkillPaths(width, []skill.Root{{
		Dir:      "/Users/me/projects/really/deep/path/to/.reasonix/skills",
		Scope:    skill.ScopeProject,
		Priority: 0,
		Status:   skill.StatusMissing,
	}})
	assertLinesWithin(t, got, width)
}

func TestRenderMCPStatusStaysWithinWidth(t *testing.T) {
	width := 72
	got := renderMCPStatus(width,
		[]plugin.ServerStatus{{Name: "a-very-long-server-name-that-should-not-wrap", Transport: "stdio", Tools: 12}},
		[]plugin.Prompt{{Server: "a-very-long-server-name-that-should-not-wrap", Name: "mcp__server__prompt_with_a_really_long_name", Description: strings.Repeat("describe ", 20)}},
		[]plugin.Resource{{Server: "a-very-long-server-name-that-should-not-wrap", URI: "file:///Users/me/project/docs/really/deep/resource.md", Name: strings.Repeat("resource ", 20)}},
		[]plugin.Failure{{Name: "another-very-long-server-name-that-should-not-wrap", Transport: "stdio", Error: strings.Repeat("failure ", 20)}},
	)
	assertLinesWithin(t, got, width)
}

func assertLinesWithin(t *testing.T, s string, width int) {
	t.Helper()
	for i, line := range strings.Split(s, "\n") {
		if got := visibleWidth(line); got > width {
			t.Fatalf("line %d exceeds width %d with %d cols:\n%s\n\nfull output:\n%s", i+1, width, got, line, s)
		}
		if strings.Contains(line, "\n") {
			t.Fatalf("line %d unexpectedly contains newline: %q", i+1, line)
		}
	}
}
