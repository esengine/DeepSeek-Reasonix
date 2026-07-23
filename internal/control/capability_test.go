package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/capability"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

type capabilityRecordingRunner struct {
	input string
}

func TestEconomyRoutesOnlyEconomyEligibleSkills(t *testing.T) {
	runner := &capabilityRecordingRunner{}
	reg := tool.NewRegistry()
	reg.Add(capabilityTestTool{name: "run_skill"})
	c := New(Options{
		Runner: runner,
		Skills: []skill.Skill{
			{Name: "economy-review", Description: "review code", Triggers: []string{"review code"}, Profiles: []string{"economy"}},
			{Name: "balanced-review", Description: "review code", Triggers: []string{"review code"}, Profiles: []string{"balanced"}},
		},
		Registry:       reg,
		RuntimeProfile: capability.ProfileEconomy,
	})

	if err := c.Run(context.Background(), "review code"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(runner.input, "skill:economy-review prefer") {
		t.Fatalf("economy skill missing from route:\n%s", runner.input)
	}
	if strings.Contains(runner.input, "skill:balanced-review") {
		t.Fatalf("balanced-only skill leaked into economy route:\n%s", runner.input)
	}
}

func (r *capabilityRecordingRunner) Run(_ context.Context, input string) error {
	r.input = input
	return nil
}

type capabilityTestTool struct{ name string }

func (t capabilityTestTool) Name() string { return t.name }
func (t capabilityTestTool) Description() string {
	return "test tool"
}
func (t capabilityTestTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t capabilityTestTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}
func (t capabilityTestTool) ReadOnly() bool { return true }

func TestRunInjectsCapabilityRouteForRelevantSkill(t *testing.T) {
	runner := &capabilityRecordingRunner{}
	reg := tool.NewRegistry()
	reg.Add(capabilityTestTool{name: "run_skill"})
	c := New(Options{
		Runner: runner,
		Skills: []skill.Skill{{
			Name:        "review",
			Description: "review code",
			Scope:       skill.ScopeBuiltin,
		}},
		Registry: reg,
	})

	if err := c.Run(context.Background(), "帮我看看这段代码有没有问题"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(runner.input, `<capability-route version="1">`) ||
		!strings.Contains(runner.input, "skill:review prefer") {
		t.Fatalf("input missing capability route:\n%s", runner.input)
	}
	if got := StripComposePrefixes(runner.input); got != "帮我看看这段代码有没有问题" {
		t.Fatalf("StripComposePrefixes = %q", got)
	}
}

func TestCreateSkillWritesThroughAndIsImmediatelyReadable(t *testing.T) {
	home := t.TempDir()
	st := skill.New(skill.Options{HomeDir: home, DisableBuiltins: true})
	c := New(Options{AllSkillStore: st, SkillStore: st})

	content := skill.RenderSkillFile(skill.SkillFileOptions{
		Name: "helper", Description: "a helper", Body: "be helpful",
		RunAs: skill.RunSubagent, Invocation: "manual",
	})
	path, err := c.CreateSkill("helper", skill.ScopeGlobal, content)
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	if path == "" {
		t.Fatal("CreateSkill returned empty path")
	}

	// No rebuild — the live store re-scans on every call.
	sk, found := c.RunSkill("/helper do a thing")
	if !found {
		t.Fatal("newly created skill should be immediately invocable by name")
	}
	if !strings.Contains(sk, "be helpful") {
		t.Fatalf("rendered skill body missing from RunSkill output: %s", sk)
	}
}

func TestCreateSkillRefusesWithoutWritableStore(t *testing.T) {
	c := New(Options{Skills: []skill.Skill{}, AllSkills: []skill.Skill{}})
	if _, err := c.CreateSkill("x", skill.ScopeGlobal, "---\ndescription: x\n---\nbody"); err == nil {
		t.Error("CreateSkill without a writable store should error")
	}
	if err := c.DeleteSkill("x", skill.ScopeGlobal); err == nil {
		t.Error("DeleteSkill without a writable store should error")
	}
}

func TestUpdateSkillOverwritesAndIsImmediatelyReadable(t *testing.T) {
	home := t.TempDir()
	st := skill.New(skill.Options{HomeDir: home, DisableBuiltins: true})
	c := New(Options{AllSkillStore: st, SkillStore: st})

	if _, err := c.CreateSkill("helper", skill.ScopeGlobal, skill.RenderSkillFile(skill.SkillFileOptions{
		Name: "helper", Description: "v1", Body: "old", RunAs: skill.RunSubagent, Invocation: "manual",
	})); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	if err := c.UpdateSkill("helper", skill.ScopeGlobal, skill.RenderSkillFile(skill.SkillFileOptions{
		Name: "helper", Description: "v2", Body: "new", RunAs: skill.RunSubagent, Invocation: "manual",
	})); err != nil {
		t.Fatalf("UpdateSkill: %v", err)
	}
	for _, sk := range c.AllSkills() {
		if sk.Name == "helper" {
			if sk.Description != "v2" || sk.Body != "new" {
				t.Fatalf("update did not take effect: description=%q body=%q", sk.Description, sk.Body)
			}
			return
		}
	}
	t.Fatal("helper missing from AllSkills after update")
}

func TestDeleteSkillRemovesLiveEntry(t *testing.T) {
	home := t.TempDir()
	st := skill.New(skill.Options{HomeDir: home, DisableBuiltins: true})
	c := New(Options{AllSkillStore: st, SkillStore: st})

	content := skill.RenderSkillFile(skill.SkillFileOptions{Name: "temp", Description: "temp", Body: "b"})
	if _, err := c.CreateSkill("temp", skill.ScopeGlobal, content); err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	if err := c.DeleteSkill("temp", skill.ScopeGlobal); err != nil {
		t.Fatalf("DeleteSkill: %v", err)
	}
	for _, sk := range c.AllSkills() {
		if sk.Name == "temp" {
			t.Fatal("deleted skill still present in AllSkills")
		}
	}
}
