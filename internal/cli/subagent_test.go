package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/skill"
)

func TestSubagentProfileCLIManageRoundTrip(t *testing.T) {
	isolateCLIConfigHome(t)
	project := t.TempDir()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	out := captureStdout(t, func() {
		if rc := subagentCommand([]string{
			"create", "helper", "--description", "Initial helper", "--prompt", "Initial prompt",
			"--model", "provider/model", "--effort", "high", "--tools", "read_file,grep,read_file", "--color", "orange",
		}); rc != 0 {
			t.Fatalf("create rc = %d", rc)
		}
	})
	if !strings.Contains(out, "created subagent profile") {
		t.Fatalf("create output = %q", out)
	}
	store := newCLISubagentStore()
	sk, ok := store.Read("helper")
	if !ok {
		t.Fatal("created profile was not discovered")
	}
	if sk.Scope != skill.ScopeProject || sk.RunAs != skill.RunSubagent || sk.Invocation != "manual" ||
		sk.Description != "Initial helper" || sk.Body != "Initial prompt" || sk.Model != "provider/model" ||
		sk.Effort != "high" || sk.Color != "orange" || strings.Join(sk.AllowedTools, ",") != "read_file,grep" {
		t.Fatalf("created profile = %+v", sk)
	}

	if rc := subagentCommand([]string{
		"edit", "helper", "--description", "Updated helper", "--prompt", "Updated prompt", "--model=", "--tools=",
	}); rc != 0 {
		t.Fatalf("edit rc = %d", rc)
	}
	sk, ok = store.Read("helper")
	if !ok || sk.Description != "Updated helper" || sk.Body != "Updated prompt" || sk.Model != "" || len(sk.AllowedTools) != 0 {
		t.Fatalf("updated profile = %+v, found=%v", sk, ok)
	}

	out = captureStdout(t, func() {
		if rc := subagentCommand([]string{"list"}); rc != 0 {
			t.Fatalf("list rc = %d", rc)
		}
	})
	if !strings.Contains(out, "helper") || !strings.Contains(out, "project, manual") || !strings.Contains(out, "Updated helper") {
		t.Fatalf("list output = %q", out)
	}

	errOut := captureStderr(t, func() {
		if rc := subagentCommand([]string{"delete", "helper"}); rc != 2 {
			t.Fatalf("unconfirmed delete rc = %d", rc)
		}
	})
	if !strings.Contains(errOut, "--yes") {
		t.Fatalf("unconfirmed delete output = %q", errOut)
	}
	if _, ok := store.Read("helper"); !ok {
		t.Fatal("unconfirmed delete removed profile")
	}
	if rc := subagentCommand([]string{"delete", "helper", "--yes"}); rc != 0 {
		t.Fatalf("delete rc = %d", rc)
	}
	if _, ok := store.Read("helper"); ok {
		t.Fatal("confirmed delete left profile behind")
	}
}

func TestSubagentProfileCLIRejectsBuiltinCollisionAndRichSkillEdit(t *testing.T) {
	isolateCLIConfigHome(t)
	project := t.TempDir()
	original, _ := os.Getwd()
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	errOut := captureStderr(t, func() {
		if rc := subagentCommand([]string{"create", "review", "--description", "d", "--prompt", "p"}); rc != 1 {
			t.Fatalf("builtin collision rc = %d", rc)
		}
	})
	if !strings.Contains(errOut, "already exists") {
		t.Fatalf("builtin collision output = %q", errOut)
	}

	path := filepath.Join(project, ".reasonix", "skills", "rich", skill.SkillFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\ndescription: rich\nrunAs: subagent\ninvocation: manual\nread-only: true\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	errOut = captureStderr(t, func() {
		if rc := subagentCommand([]string{"edit", "rich", "--description", "changed"}); rc != 1 {
			t.Fatalf("rich edit rc = %d", rc)
		}
	})
	if !strings.Contains(errOut, "does not manage") {
		t.Fatalf("rich edit output = %q", errOut)
	}
}

func TestSubagentProfileCLIRejectsReservedAndCustomCommandNames(t *testing.T) {
	isolateCLIConfigHome(t)
	project := t.TempDir()
	original, _ := os.Getwd()
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	commandPath := filepath.Join(project, ".reasonix", "commands", "formatter.md")
	if err := os.MkdirAll(filepath.Dir(commandPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(commandPath, []byte("---\ndescription: format\n---\nformat"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"clear", "mcp__server__prompt", "formatter"} {
		errOut := captureStderr(t, func() {
			if rc := subagentCommand([]string{"create", name, "--description", "d", "--prompt", "p"}); rc != 1 {
				t.Fatalf("create %q rc = %d", name, rc)
			}
		})
		if !strings.Contains(errOut, "slash command namespace") {
			t.Fatalf("create %q output = %q", name, errOut)
		}
	}
}

func TestSubagentProfileCLIEditBuiltinModelOverride(t *testing.T) {
	isolateCLIConfigHome(t)
	cfg := config.Default()
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	if rc := subagentCommand([]string{"edit", "review", "--model", "deepseek-pro"}); rc != 0 {
		t.Fatalf("builtin edit rc = %d", rc)
	}
	loaded := config.LoadForEdit(config.UserConfigPath())
	if got := subagentOverride(loaded.Agent.SubagentModels, "review"); !strings.HasPrefix(got, "deepseek-pro/") {
		t.Fatalf("review model override = %q", got)
	}
	out := captureStdout(t, func() {
		if rc := subagentCommand([]string{"list"}); rc != 0 {
			t.Fatalf("list rc = %d", rc)
		}
	})
	if !strings.Contains(out, "review") || !strings.Contains(out, "model=deepseek-pro/") {
		t.Fatalf("list did not show built-in override:\n%s", out)
	}
	if rc := subagentCommand([]string{"edit", "review", "--model="}); rc != 0 {
		t.Fatalf("builtin clear rc = %d", rc)
	}
	loaded = config.LoadForEdit(config.UserConfigPath())
	if got := subagentOverride(loaded.Agent.SubagentModels, "review"); got != "" {
		t.Fatalf("review model override survived clear: %q", got)
	}
}

func TestSubagentProfileCLIRunAndTrySelectIsolatedRunners(t *testing.T) {
	previous := setupSubagentCommand
	t.Cleanup(func() { setupSubagentCommand = previous })

	var normalCalls, readOnlyCalls int
	var normalTask, tryTask string
	setupSubagentCommand = func(context.Context, string, int, bool, event.Sink) (*control.Controller, error) {
		return control.New(control.Options{
			Skills: []skill.Skill{{Name: "helper", RunAs: skill.RunSubagent, Invocation: "manual", Scope: skill.ScopeGlobal}},
			SkillRunner: func(_ context.Context, _ skill.Skill, task string, opts skill.SubagentRunOptions) (string, error) {
				normalCalls++
				normalTask = task
				if !opts.HostInitiated {
					t.Fatal("run did not mark host-initiated invocation")
				}
				return "run answer", nil
			},
			ReadOnlySkillRunner: func(_ context.Context, _ skill.Skill, task string, opts skill.SubagentRunOptions) (string, error) {
				readOnlyCalls++
				tryTask = task
				if !opts.HostInitiated {
					t.Fatal("try did not mark host-initiated invocation")
				}
				return "try answer", nil
			},
		}), nil
	}

	out := captureStdout(t, func() {
		if rc := subagentCommand([]string{"run", "helper", "inspect auth"}); rc != 0 {
			t.Fatalf("run rc = %d", rc)
		}
	})
	if strings.TrimSpace(out) != "run answer" || normalCalls != 1 || normalTask != "inspect auth" || readOnlyCalls != 0 {
		t.Fatalf("run output=%q normal=%d task=%q readonly=%d", out, normalCalls, normalTask, readOnlyCalls)
	}

	out = captureStdout(t, func() {
		if rc := subagentCommand([]string{"try", "helper", "inspect only"}); rc != 0 {
			t.Fatalf("try rc = %d", rc)
		}
	})
	if strings.TrimSpace(out) != "try answer" || readOnlyCalls != 1 || tryTask != "inspect only" {
		t.Fatalf("try output=%q readonly=%d task=%q", out, readOnlyCalls, tryTask)
	}
}

func TestRootHelpListsSubagentCommand(t *testing.T) {
	out := captureStdout(t, func() {
		if rc := Run([]string{"help"}, "test"); rc != 0 {
			t.Fatalf("help rc = %d", rc)
		}
	})
	if !strings.Contains(out, "reasonix subagent <list|create|edit|delete|try|run>") {
		t.Fatalf("help output missing subagent command:\n%s", out)
	}
}
