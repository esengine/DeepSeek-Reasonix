package boot

import (
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
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

func TestSubagentEffortRefHonorsPrecedence(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.SubagentEffort = "high"
	cfg.Agent.SubagentEfforts = map[string]string{"review": "max"}

	got := subagentEffortRef(cfg, skill.Skill{
		Name:   "review",
		RunAs:  skill.RunSubagent,
		Effort: "low",
	})
	if got != "max" {
		t.Fatalf("per-skill effort config should override skill frontmatter and default, got %q", got)
	}

	got = subagentEffortRef(cfg, skill.Skill{
		Name:   "custom",
		RunAs:  skill.RunSubagent,
		Effort: "medium",
	})
	if got != "medium" {
		t.Fatalf("skill frontmatter effort should override default config, got %q", got)
	}

	got = subagentEffortRef(cfg, skill.Skill{Name: "other", RunAs: skill.RunSubagent})
	if got != "high" {
		t.Fatalf("default subagent effort = %q, want high", got)
	}
}

func TestSubagentEffortRefAcceptsToolNameAliases(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.SubagentEfforts = map[string]string{"security_review": "max"}

	got := subagentEffortRef(cfg, skill.Skill{Name: "security-review", RunAs: skill.RunSubagent})
	if got != "max" {
		t.Fatalf("security_review alias should configure security-review effort, got %q", got)
	}
}

func TestSubagentEffectiveIdentityUsesResolvedModelAndEffort(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = []config.ProviderEntry{{
		Name:             "custom",
		Kind:             "openai",
		Models:           []string{"alpha", "beta"},
		Default:          "beta",
		SupportedEfforts: []string{"low", "high"},
		DefaultEffort:    "high",
	}}
	base, ok := cfg.ResolveModel("custom")
	if !ok {
		t.Fatal("custom provider should resolve")
	}

	model, effort := subagentEffectiveIdentity(cfg, "custom", base, "", "")
	if model != "custom/beta" || effort != "high" {
		t.Fatalf("identity = %q/%q, want custom/beta/high", model, effort)
	}

	model, effort = subagentEffectiveIdentity(cfg, "custom", base, "alpha", "low")
	if model != "custom/alpha" || effort != "low" {
		t.Fatalf("override identity = %q/%q, want custom/alpha/low", model, effort)
	}
}

func TestNewSubagentStoreRequiresSessionDir(t *testing.T) {
	if got, err := newSubagentStore(""); err != nil || got != nil {
		if err != nil {
			t.Fatalf("empty session dir error = %v", err)
		}
		t.Fatalf("empty session dir should disable subagent store, got %#v", got)
	}
	if got, err := newSubagentStore(t.TempDir()); err != nil || got == nil {
		if err != nil {
			t.Fatalf("non-empty session dir error = %v", err)
		}
		t.Fatal("non-empty session dir should create subagent store")
	}
}

func TestNewSubagentStoreCleansStaleRunningRefs(t *testing.T) {
	sessionDir := t.TempDir()
	store := agent.NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	spec := agent.SubagentSpec{
		Kind:          "task",
		Name:          "task",
		WorkspaceRoot: t.TempDir(),
		ParentSession: "parent-session",
		SystemPrompt:  "sys",
		Registry:      tool.NewRegistry(),
		Model:         "base-model",
	}
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	if err := store.MarkRunning(run); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	ref := run.Ref
	run.Release()

	got, err := newSubagentStore(sessionDir)
	if err != nil {
		t.Fatalf("newSubagentStore: %v", err)
	}
	if got == nil {
		t.Fatal("newSubagentStore returned nil")
	}
	meta, err := got.LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.Status != agent.SubagentInterrupted {
		t.Fatalf("status = %q, want interrupted", meta.Status)
	}
}
