package boot

import (
	"errors"
	"strings"
	"testing"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/ext/skill"
)

func effortModelsConfig(globalEffort string) *config.Config {
	cfg := config.Default()
	cfg.Agent.SubagentEffort = globalEffort
	cfg.Providers = []config.ProviderEntry{
		{Name: "parent", Kind: "openai", Models: []string{"p"}, Default: "p",
			SupportedEfforts: []string{"low", "high", "max"}, DefaultEffort: "high"},
		{Name: "narrow", Kind: "openai", Models: []string{"n"}, Default: "n",
			SupportedEfforts: []string{"low", "high"}, DefaultEffort: "high"},
		{Name: "wide", Kind: "openai", Models: []string{"w"}, Default: "w",
			SupportedEfforts: []string{"low", "high", "max"}, DefaultEffort: "high"},
		{Name: "mimo", Kind: "openai", BaseURL: "https://api.xiaomimimo.com/v1", Models: []string{"mimo-v2.5-pro"}, Default: "mimo-v2.5-pro"},
		{Name: "minimax", Kind: "openai", BaseURL: "https://api.minimaxi.com/v1", Models: []string{"MiniMax-M3"}, Default: "MiniMax-M3"},
	}
	return cfg
}

func newEffortSub(t *testing.T, cfg *config.Config, sink event.Sink) (subagentConfig, *effortSelectionResolver) {
	t.Helper()
	parent, ok := cfg.ResolveModel("parent/p")
	if !ok {
		t.Fatal("parent/p should resolve")
	}
	resolver := &effortSelectionResolver{}
	return newSubagentConfig(Options{Sink: sink}, cfg, parent, "parent/p", resolver, netclient.ProxySpec{}, nil), resolver
}

func TestInheritedEffortIsResolvedPerExecutingModel(t *testing.T) {
	for _, tc := range []struct {
		name, global, model, want string
	}{
		{"parent keeps supported level", "max", "", "max"},
		{"model that declares the level keeps it", "max", "wide/w", "max"},
		{"narrow model drops a level it lacks", "max", "narrow/n", ""},
		{"narrow model keeps a shared level", "low", "narrow/n", "low"},
		{"MiMo drops max instead of remapping it", "max", "mimo/mimo-v2.5-pro", ""},
		{"MiMo keeps a level its protocol declares", "low", "mimo/mimo-v2.5-pro", "low"},
		{"MiniMax drops max instead of disabling thinking", "max", "minimax/MiniMax-M3", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub, _ := newEffortSub(t, effortModelsConfig(tc.global), nil)
			if got := sub.inheritedFor(tc.model); got != tc.want {
				t.Fatalf("inheritedFor(%q) with subagent_effort=%q = %q, want %q", tc.model, tc.global, got, tc.want)
			}
		})
	}
}

func TestInheritedEffortDroppedForOtherModelIsAnnouncedOnce(t *testing.T) {
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) { events = append(events, e) })
	sub, _ := newEffortSub(t, effortModelsConfig("max"), sink)
	for range 3 {
		sub.inheritedFor("narrow/n")
	}
	if len(events) != 1 || !strings.Contains(events[0].Detail, `"narrow/n"`) || !strings.Contains(events[0].Detail, "agent.subagent_effort") {
		t.Fatalf("events = %+v, want one warning naming agent.subagent_effort and narrow/n", events)
	}
}

func TestSkillEffortIsResolvedForTheSkillsOwnModel(t *testing.T) {
	cfg := effortModelsConfig("max")
	sub, _ := newEffortSub(t, cfg, nil)
	for _, tc := range []struct {
		name string
		sk   skill.Skill
		want string
	}{
		{"skill frontmatter model without the level", skill.Skill{Name: "r", RunAs: skill.RunSubagent, Model: "narrow/n"}, ""},
		{"skill frontmatter model with the level", skill.Skill{Name: "r", RunAs: skill.RunSubagent, Model: "wide/w"}, "max"},
		{"skill without its own model follows the parent", skill.Skill{Name: "r", RunAs: skill.RunSubagent}, "max"},
		{"explicit skill effort stays explicit", skill.Skill{Name: "r", RunAs: skill.RunSubagent, Model: "narrow/n", Effort: "low"}, "low"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := subagentEffortRef(cfg, tc.sk, sub.inheritedFor); got != tc.want {
				t.Fatalf("skill effort = %q, want %q", got, tc.want)
			}
		})
	}

	cfg.Agent.SubagentModels = map[string]string{"r": "narrow/n"}
	got := subagentEffortRef(cfg, skill.Skill{Name: "r", RunAs: skill.RunSubagent}, sub.inheritedFor)
	if got != "" {
		t.Fatalf("subagent_models override on a skill: inherited effort = %q, want dropped", got)
	}
}

func TestEverySelectionPathAgreesOnTheSameModelAndLevel(t *testing.T) {
	cfg := effortModelsConfig("max")
	cfg.Agent.SubagentModels = map[string]string{"task": "narrow/n"}
	sub, resolver := newEffortSub(t, cfg, nil)

	fromTask := sub.inheritedFor(sub.taskModel)
	fromSkill := subagentEffortRef(cfg, skill.Skill{Name: "x", RunAs: skill.RunSubagent, Model: "narrow/n"}, sub.inheritedFor)
	if fromTask != fromSkill || fromTask != "" {
		t.Fatalf("task path %q and skill path %q must agree on dropping max for narrow/n", fromTask, fromSkill)
	}
	if _, _, _, err := sub.resolveProvider(sub.taskModel, fromTask); err != nil {
		t.Fatalf("resolve with the dropped default: %v", err)
	}
	if got := resolver.selections[0].Effort; got != nil && *got == "max" {
		t.Fatalf("provider selection carries max for a model that does not declare it")
	}
}

func TestExplicitUnsupportedEffortFailsWithTypedError(t *testing.T) {
	cfg := effortModelsConfig("")
	cfg.Agent.SubagentEfforts = map[string]string{"task": "max"}
	parent, _ := cfg.ResolveModel("parent/p")
	sub := newSubagentConfig(Options{}, cfg, parent, "parent/p", nil, netclient.ProxySpec{}, nil)
	_, _, _, err := sub.resolveProvider("narrow/n", sub.taskEffort)
	if !errors.Is(err, config.ErrEffortUnsupported) {
		t.Fatalf("err = %v, want errors.Is ErrEffortUnsupported", err)
	}
	if !strings.Contains(err.Error(), "narrow/n") || !strings.Contains(err.Error(), `"max"`) {
		t.Fatalf("err = %q, want it to name the model and the level", err)
	}
}
