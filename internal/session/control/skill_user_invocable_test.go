package control

import (
	"context"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
	"strings"
	"testing"

	"reasonix/internal/ext/skill"
)

func userHiddenController() *Controller {
	return New(Options{Skills: []skill.Skill{
		{Name: "ctx-notes", Description: "model only", Body: "CTX", Scope: skill.ScopeProject,
			InvocationFlags: skill.InvocationFlags{DisableUserInvocation: true}},
		{Name: "ctx-sub", Description: "model only worker", Body: "SUB", Scope: skill.ScopeProject, RunAs: skill.RunSubagent,
			InvocationFlags: skill.InvocationFlags{DisableUserInvocation: true}},
		{Name: "plain-one", Description: "ordinary", Body: "PLAIN", Scope: skill.ScopeProject},
	}})
}

// Every user-side way to start a skill by name must treat user-invocable:false
// as unknown, while the model-side registry still holds the skill.
func TestUserInvocableFalseIsRefusedOnEveryUserPath(t *testing.T) {
	c := userHiddenController()
	defer c.Close()

	if _, _, ok := c.resolveSkillInvocation("/ctx-notes go"); ok {
		t.Error("typed /name resolved a user-invocable:false skill")
	}
	if _, found := c.RunSkill("/ctx-notes"); found {
		t.Error("RunSkill rendered a user-invocable:false skill")
	}
	if _, err := c.prepareInvocationTurn("x", []InvocationRequest{{Name: "ctx-notes", Kind: "skill"}}); err == nil {
		t.Error("an invocation chip resolved a user-invocable:false skill")
	}
	if _, err := c.RunSubagentProfile(context.Background(), "ctx-sub", "task", false); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("subagent run by argument = %v, want unknown", err)
	}
	text := c.skillListText()
	if strings.Contains(text, "/ctx-") || !strings.Contains(text, "ctx-notes [model-only") || !strings.Contains(text, "/plain-one") {
		t.Errorf("/skills listing must label model-only skills and offer no /name for them: %q", text)
	}
	for _, sk := range c.SlashSkills() {
		if strings.HasPrefix(sk.Name, "ctx-") {
			t.Errorf("SlashSkills carries %s", sk.Name)
		}
	}
	data := c.CompletionData("en")
	for _, sk := range data.Skills {
		if strings.HasPrefix(sk.Name, "ctx-") {
			t.Errorf("the palette argument data carries %s", sk.Name)
		}
	}
	for _, it := range data.Names {
		if strings.Contains(it.Label, "ctx-") {
			t.Errorf("the palette offers %s", it.Label)
		}
	}
	if !skillListed(c.Skills(), "ctx-notes") {
		t.Error("the model-side registry lost the skill")
	}
}

func skillListed(skills []skill.Skill, name string) bool {
	for _, sk := range skills {
		if sk.Name == name {
			return true
		}
	}
	return false
}

func TestReloadCommandsKeepsUserOnlySkillsOutOfSlashCommandTool(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	reg := tool.NewRegistry()
	c := New(Options{
		Sink: &typedNilControllerSink{}, Registry: reg, WorkspaceRoot: testenv.TempDir(t),
		Skills: []skill.Skill{
			{Name: "ship-it", Description: "deploys", Body: "SHIP BODY", InvocationFlags: skill.InvocationFlags{DisableModelInvocation: true}},
			{Name: "plain-one", Description: "ordinary", Body: "PLAIN BODY"},
		},
	})
	defer c.Close()
	if err := c.ReloadCommands(context.Background()); err != nil {
		t.Fatal(err)
	}
	sc, ok := reg.Get("slash_command")
	if !ok {
		t.Fatal("slash_command not registered")
	}
	list, _ := sc.Execute(context.Background(), []byte(`{"command":"list"}`))
	if strings.Contains(list, "ship-it") || !strings.Contains(list, "plain-one") {
		t.Fatalf("slash_command list after reload:\n%s", list)
	}
	out, err := sc.Execute(context.Background(), []byte(`{"command":"ship-it"}`))
	if err == nil || strings.Contains(out, "SHIP BODY") || strings.Contains(err.Error(), "ship-it\"; available: ship") {
		t.Fatalf("slash_command ship-it = %q, %v", out, err)
	}
}
