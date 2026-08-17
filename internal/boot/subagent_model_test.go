package boot

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/ablation"
	"reasonix/internal/agent"
	"reasonix/internal/billing"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

type staticResolver struct {
	catalog []provider.Descriptor
}

func (r *staticResolver) Catalog() []provider.Descriptor { return r.catalog }
func (r *staticResolver) Resolve(provider.Selection) (provider.Provider, error) {
	return nil, nil
}

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

	model, effort := subagentEffectiveIdentity(cfg, nil, "custom", base, "", "")
	if model != "custom/beta" || effort != "high" {
		t.Fatalf("identity = %q/%q, want custom/beta/high", model, effort)
	}

	model, effort = subagentEffectiveIdentity(cfg, nil, "custom", base, "alpha", "low")
	if model != "custom/alpha" || effort != "low" {
		t.Fatalf("override identity = %q/%q, want custom/alpha/low", model, effort)
	}
}

func TestSubagentEffectiveIdentityUsesAuthoritativeExternalResolver(t *testing.T) {
	cfg := config.Default()
	base := &config.ProviderEntry{Name: "openai", Model: "gpt"}
	resolver := &staticResolver{catalog: []provider.Descriptor{{
		Ref: "anthropic/claude-sonnet", DisplayName: "anthropic", Model: "claude-sonnet",
		Efforts: []string{"low", "high"}, DefaultEffort: "high",
	}}}

	model, effort := subagentEffectiveIdentity(cfg, resolver, "openai/gpt", base, "anthropic/claude-sonnet", "high")
	if model != "anthropic/claude-sonnet" || effort != "high" {
		t.Fatalf("identity = %q/%q, want anthropic/claude-sonnet/high", model, effort)
	}
}

func TestSubagentSkillOptionsPreserveScheduledQuoteContext(t *testing.T) {
	ratedAt := time.Date(2026, time.August, 17, 4, 0, 0, 0, time.UTC)
	quoteCtx := &event.QuoteContext{
		Now: func() time.Time { return ratedAt },
		PricingContextForModel: func(modelRef string) billing.PricingContext {
			if modelRef != "deepseek-pro/deepseek-v4-pro" {
				t.Fatalf("pricing context model = %q", modelRef)
			}
			return billing.PricingContext{
				ProviderKind: "deepseek",
				ModelID:      "deepseek-v4-pro",
				BillingMode:  billing.BillingModePAYG,
				ScheduleID:   billing.ScheduleDeepSeekV4August2026,
			}
		},
	}
	factory := newSubagentSkillOptionsFactory(config.AgentConfig{}, quoteCtx, nil, 0, 1, ablation.Set{}, nil, nil)
	opts := factory(context.Background(), 5, &provider.Pricing{CacheHit: 0.30, Input: 9, Output: 27, Currency: "CNY"}, 1_000_000, 1)
	if opts.QuoteContext != quoteCtx {
		t.Fatal("skill subagent did not retain the host QuoteContext")
	}

	quote := event.EnsureCostQuote(event.Event{
		Kind:        event.Usage,
		ModelRef:    "deepseek-pro/deepseek-v4-pro",
		Usage:       &provider.Usage{PromptTokens: 1_000_000, TotalTokens: 1_000_000},
		Pricing:     opts.Pricing,
		UsageSource: event.UsageSourceSubagent,
	}, opts.QuoteContext)
	if quote == nil || quote.RateBand != billing.RateBandOffPeak || quote.Original.Amount != "4.5" {
		t.Fatalf("skill subagent quote = %+v, want CNY 4.5 off-peak", quote)
	}
}

func TestNewSubagentStoreRequiresSessionDir(t *testing.T) {
	if got, err := newSubagentStore("", nil); err != nil || got != nil {
		if err != nil {
			t.Fatalf("empty session dir error = %v", err)
		}
		t.Fatalf("empty session dir should disable subagent store, got %#v", got)
	}
	if got, err := newSubagentStore(t.TempDir(), nil); err != nil || got == nil {
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

	got, err := newSubagentStore(sessionDir, nil)
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
