package boot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

func TestEffectSharedProjectSkillAcrossWorktreesAndHomes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for the linked-worktree fixture")
	}
	isolateConfigHome(t)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	owned := robustTempDir(t)
	main := filepath.Join(owned, "main")
	linked := filepath.Join(owned, "linked")
	outside := filepath.Join(owned, "outside")
	const projectBody = "Read the current diff and the files it changes.\nReport correctness issues with a file path, a concrete trigger, and the expected behavior.\nSeparate findings from verification that still needs to run."
	const personalBody = "PERSONAL REVIEW BODY"
	const relative = ".agents/skills/team-review/SKILL.md"
	const shared = "---\nname: team-review\ndescription: Review local changes using the team checklist\n---\n" + projectBody + "\n"
	writeFile(t, main, relative, shared)
	git := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "--template=", main)
	git("-C", main, "add", relative)
	git("-C", main, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "Add shared review skill")
	git("-C", main, "worktree", "add", "--detach", linked, "HEAD")
	if key := config.ProjectKey(main); key == "" || config.ProjectKey(linked) != key {
		t.Fatal("fixture worktrees do not share the canonical project identity")
	}
	for _, root := range []string{main, linked, outside} {
		writeFile(t, root, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[environment]
enabled = false

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-shared-project-skill"
model = "x"
`)
	}
	homes := []string{filepath.Join(owned, "alice"), filepath.Join(owned, "bob")}
	for _, home := range homes {
		writeFile(t, home, "skills/team-review/SKILL.md", "---\nname: team-review\ndescription: Personal review fallback\n---\n"+personalBody)
	}
	rec := &effectRecordingProvider{}
	provider.Register("boot-shared-project-skill", func(provider.Config) (provider.Provider, error) { return rec, nil })
	var prefix string
	runPhase := func(t *testing.T, home, root, body string, before func(*control.Controller)) {
		t.Helper()
		closeBootTestHistoryCatalog(t)
		t.Setenv("REASONIX_HOME", home)
		t.Setenv("REASONIX_STATE_HOME", home)
		t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))
		t.Cleanup(func() { closeBootTestHistoryCatalog(t) })
		t.Chdir(root)
		approveWorkspace(t, root)
		ctrl, err := Build(t.Context(), Options{Sink: event.Discard, WorkspaceRoot: root})
		if err != nil {
			t.Fatal(err)
		}
		defer ctrl.Close()
		if before != nil {
			before(ctrl)
		}
		visible := false
		for _, item := range ctrl.CompletionData("en").Names {
			if item.Label == "/team-review" {
				visible = true
				if item.Kind != "skill" {
					t.Fatalf("shared review completion = %+v", item)
				}
			}
		}
		if visible != (body != "") {
			t.Fatalf("review completion visible=%t, expected body=%q", visible, body)
		}
		const input = "/team-review inspect this change"
		if sent, found := ctrl.RunSkill(input); found != (body != "") || (found && !strings.Contains(sent, body)) {
			t.Fatalf("RunSkill = %q, found=%t, expected body=%q", sent, found, body)
		}
		if body != "" {
			ctrl.EnsureSessionPath()
		}
		count := len(rec.requests())
		ctrl.SubmitHTTPOptions(input, control.SubmitOptions{RefuseUnknownSlash: true})
		if body == "" {
			if ctrl.Running() || len(rec.requests()) != count {
				t.Fatal("project-disabled review skill started a provider turn")
			}
			return
		}
		waitForCond(t, "shared review provider request", 10*time.Second, func() bool { return len(rec.requests()) > count })
		waitForCond(t, "shared review completion", 10*time.Second, func() bool { return !ctrl.Running() })
		reqs := rec.requests()
		last := reqs[len(reqs)-1]
		var user string
		for _, msg := range last.Messages {
			if msg.Role == provider.RoleUser {
				user = msg.Content
			}
		}
		if !strings.Contains(user, body) || strings.Count(user, "<skill-pin name=\"team-review\">") != 1 || !strings.Contains(user, "Arguments: inspect this change") {
			t.Fatalf("shared review invocation did not reach provider:\n%s", user)
		}
		if body == projectBody && strings.Contains(user, personalBody) || body == personalBody && strings.Contains(user, projectBody) {
			t.Fatalf("review invocation mixed project and personal bodies:\n%s", user)
		}
		current := systemMessage(last.Messages)
		if strings.Contains(current, projectBody) || strings.Contains(current, personalBody) {
			t.Fatal("review body leaked into the cached system prefix")
		}
		if prefix == "" {
			prefix = current
		} else if prefix != current {
			t.Fatal("shared review roots or personal state changed the cached system prefix")
		}
	}
	setEnabled := func(t *testing.T, enabled bool) func(*control.Controller) {
		return func(ctrl *control.Controller) {
			if err := ctrl.SetSkillEnabled("team-review", config.ActivationProject, enabled); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Run("alice-main", func(t *testing.T) { runPhase(t, homes[0], main, projectBody, nil) })
	t.Run("alice-disable-live", func(t *testing.T) { runPhase(t, homes[0], main, "", setEnabled(t, false)) })
	t.Run("alice-linked-disabled", func(t *testing.T) { runPhase(t, homes[0], linked, "", nil) })
	t.Run("alice-restarted-disabled", func(t *testing.T) { runPhase(t, homes[0], main, "", nil) })
	t.Run("alice-outside-fallback", func(t *testing.T) { runPhase(t, homes[0], outside, personalBody, nil) })
	t.Run("bob-main", func(t *testing.T) { runPhase(t, homes[1], main, projectBody, nil) })
	t.Run("bob-linked", func(t *testing.T) { runPhase(t, homes[1], linked, projectBody, nil) })
	if enabled, err := config.NewActivationStore(homes[0]).SkillEnabled("team-review", main, true); err != nil || enabled {
		t.Fatalf("Alice's durable project switch changed after Bob's turns: enabled=%t, err=%v", enabled, err)
	}
	t.Run("alice-enable-live", func(t *testing.T) { runPhase(t, homes[0], main, projectBody, setEnabled(t, true)) })
	t.Run("alice-linked-enabled", func(t *testing.T) { runPhase(t, homes[0], linked, projectBody, nil) })
	for _, root := range []string{main, linked} {
		if got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative))); err != nil || string(got) != shared {
			t.Fatalf("shared skill bytes changed under %s: %q, err=%v", root, got, err)
		}
		cmd := exec.CommandContext(t.Context(), "git", "-C", root, "diff", "--exit-code", "HEAD", "--", relative)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("personal switches changed tracked skill content: %v\n%s", err, out)
		}
	}
}
