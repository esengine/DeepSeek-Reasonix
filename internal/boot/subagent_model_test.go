package boot

import (
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/skill"
)

func TestSubagentModelRefUsesConfiguredDefault(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.SubagentModel = "deepseek-pro"

	got := subagentModelRef(cfg, skill.Skill{Name: "explore", RunAs: skill.RunSubagent})
	if got != "deepseek-pro" {
		t.Fatalf("subagent model = %q, want deepseek-pro", got)
	}
}

func TestSubagentModelRefHonorsPrecedence(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.SubagentModel = "mimo-pro"
	cfg.Agent.SubagentModels = map[string]string{"review": "deepseek-pro"}

	got := subagentModelRef(cfg, skill.Skill{
		Name:  "review",
		RunAs: skill.RunSubagent,
		Model: "mimo-flash",
	})
	if got != "deepseek-pro" {
		t.Fatalf("per-skill config should override skill frontmatter and default, got %q", got)
	}

	got = subagentModelRef(cfg, skill.Skill{
		Name:  "custom",
		RunAs: skill.RunSubagent,
		Model: "mimo-flash",
	})
	if got != "mimo-flash" {
		t.Fatalf("skill frontmatter should override default config, got %q", got)
	}
}

func TestSubagentModelRefAcceptsToolNameAliases(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.SubagentModels = map[string]string{"security_review": "deepseek-pro"}

	got := subagentModelRef(cfg, skill.Skill{Name: "security-review", RunAs: skill.RunSubagent})
	if got != "deepseek-pro" {
		t.Fatalf("security_review alias should configure security-review, got %q", got)
	}
}
