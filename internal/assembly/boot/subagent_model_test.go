package boot

import (
	"fmt"
	"path/filepath"
	"reasonix/internal/base/netclient"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/skill"
	"reasonix/internal/runtime/delegation"
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

func TestNewSubagentStoreRequiresSessionDir(t *testing.T) {
	if got, err := newSubagentStore("", nil); err != nil || got != nil {
		if err != nil {
			t.Fatalf("empty session dir error = %v", err)
		}
		t.Fatalf("empty session dir should disable subagent store, got %#v", got)
	}
	if got, err := newSubagentStore(robustTempDir(t), nil); err != nil || got == nil {
		if err != nil {
			t.Fatalf("non-empty session dir error = %v", err)
		}
		t.Fatal("non-empty session dir should create subagent store")
	}
}

func TestNewSubagentStoreCleansStaleRunningRefs(t *testing.T) {
	sessionDir := robustTempDir(t)
	store := delegation.NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	spec := delegation.SubagentSpec{ExecutionID: "exec-test",
		Kind:          "task",
		Name:          "task",
		WorkspaceRoot: robustTempDir(t),
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
	if meta.Status != sessionstore.SubagentInterrupted {
		t.Fatalf("status = %q, want interrupted", meta.Status)
	}
}

type recordingResolver struct{ refs []string }

func (r *recordingResolver) Catalog() []provider.Descriptor { return nil }
func (r *recordingResolver) Resolve(sel provider.Selection) (provider.Provider, error) {
	r.refs = append(r.refs, sel.Ref)
	return nil, nil
}

func TestSubagentBareModelStaysOnParentProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = []config.ProviderEntry{
		{Name: "first", Kind: "openai", Models: []string{"shared-flash", "first-only"}},
		{Name: "relay", Kind: "openai", Models: []string{"shared-flash"}},
	}
	base, ok := cfg.ResolveModel("relay/shared-flash")
	if !ok {
		t.Fatal("relay/shared-flash should resolve")
	}
	rec := &recordingResolver{}
	sub := newSubagentConfig(Options{}, cfg, base, "relay/shared-flash", rec, netclient.ProxySpec{}, nil)

	if _, _, _, err := sub.resolveProvider("shared-flash", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := sub.resolveProvider("first-only", ""); err != nil {
		t.Fatal(err)
	}
	want := []string{"relay/shared-flash", "first/first-only"}
	if len(rec.refs) != 2 || rec.refs[0] != want[0] || rec.refs[1] != want[1] {
		t.Fatalf("resolved refs = %q, want %q", rec.refs, want)
	}
	if model, _ := sub.identity("shared-flash", ""); model != "relay/shared-flash" {
		t.Fatalf("identity = %q, want relay/shared-flash", model)
	}
}

type effortSelectionResolver struct {
	selections   []provider.Selection
	rejectEffort bool
}

func (r *effortSelectionResolver) Catalog() []provider.Descriptor { return nil }
func (r *effortSelectionResolver) Resolve(selection provider.Selection) (provider.Provider, error) {
	r.selections = append(r.selections, selection)
	if r.rejectEffort && selection.Effort != nil && *selection.Effort != "" {
		return nil, fmt.Errorf("provider rejected effort %q", *selection.Effort)
	}
	return nil, nil
}

func TestInheritedGlobalEffortDoesNotReachUnsupportedExecutionModel(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.SubagentEffort = "max"
	cfg.Providers = []config.ProviderEntry{{
		Name: "custom", Kind: "openai", Models: []string{"fallback"}, Default: "fallback",
		SupportedEfforts: []string{"low", "high"}, DefaultEffort: "high",
	}}
	entry, ok := cfg.ResolveModel("custom/fallback")
	if !ok {
		t.Fatal("custom/fallback should resolve")
	}
	resolver := &effortSelectionResolver{rejectEffort: true}
	sub := newSubagentConfig(Options{}, cfg, entry, "custom/fallback", resolver, netclient.ProxySpec{}, nil)

	if sub.taskEffort != "" {
		t.Fatalf("inherited task effort = %q, want it dropped for unsupported model", sub.taskEffort)
	}
	if cfg.Agent.SubagentEffort != "max" {
		t.Fatalf("persistent subagent effort = %q, want max", cfg.Agent.SubagentEffort)
	}
	if _, _, _, err := sub.resolveProvider("", sub.taskEffort); err != nil {
		t.Fatalf("inherited effort should not make provider resolution fail: %v", err)
	}
	if len(resolver.selections) != 1 {
		t.Fatalf("provider selections = %+v, want exactly one", resolver.selections)
	}
	if got := resolver.selections[0].Effort; got != nil && *got != entry.Effort {
		t.Fatalf("provider effort = %q, want the parent's own effort %q", *got, entry.Effort)
	}
	model, effort := sub.identity("", sub.taskEffort)
	if model != "custom/fallback" || effort != "high" {
		t.Fatalf("effective identity = %q/%q, want custom/fallback/high", model, effort)
	}
}

func TestInheritedGlobalEffortDropsWhenCapabilityIsUnknown(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.SubagentEffort = "max"

	got := resolveInheritedSubagentEffort(cfg, &config.ProviderEntry{Name: "opaque", Model: "model"})
	if got.value != "" || !got.dropped {
		t.Fatalf("unknown effort capability = %+v, want dropped inherited default", got)
	}
}

func TestInheritedGlobalEffortDoesNotRemapAcrossModels(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.SubagentEffort = "max"

	entry := &config.ProviderEntry{
		Name:    "minimax",
		Kind:    "openai",
		BaseURL: "https://api.minimaxi.com/v1",
		Model:   "MiniMax-M3",
	}

	got := resolveInheritedSubagentEffort(cfg, entry)
	if got.value != "" || !got.dropped {
		t.Fatalf("inherited max = %+v, want dropped instead of remapped", got)
	}
}

func TestInheritedGlobalEffortKeepsDeepSeekContractAliases(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{raw: "off", want: "disabled"},
		{raw: "medium", want: "high"},
		{raw: "xhigh", want: "max"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			cfg := config.Default()
			cfg.Agent.SubagentEffort = tc.raw
			entry := &config.ProviderEntry{
				Name:    "deepseek",
				Kind:    "openai",
				BaseURL: "https://api.deepseek.com/v1",
				Model:   "deepseek-v4-pro",
			}

			got := resolveInheritedSubagentEffort(cfg, entry)
			if got.dropped || got.value != tc.want {
				t.Fatalf("inherited %q = %+v, want canonical %q", tc.raw, got, tc.want)
			}
			if cfg.Agent.SubagentEffort != tc.raw {
				t.Fatalf("persistent subagent effort = %q, want %q", cfg.Agent.SubagentEffort, tc.raw)
			}
		})
	}
}

func TestInheritedGlobalEffortKeepsSupportedExecutionModel(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.SubagentEffort = "max"
	cfg.Providers = []config.ProviderEntry{{
		Name: "custom", Kind: "openai", Models: []string{"supported"}, Default: "supported",
		SupportedEfforts: []string{"low", "high", "max"}, DefaultEffort: "high",
	}}
	entry, ok := cfg.ResolveModel("custom/supported")
	if !ok {
		t.Fatal("custom/supported should resolve")
	}
	resolver := &effortSelectionResolver{}
	sub := newSubagentConfig(Options{}, cfg, entry, "custom/supported", resolver, netclient.ProxySpec{}, nil)

	if sub.taskEffort != "max" || sub.inheritedEffort != "max" || sub.inheritedEffortDropped {
		t.Fatalf("inherited effort state = %q/%q/dropped=%v, want max/max/false", sub.taskEffort, sub.inheritedEffort, sub.inheritedEffortDropped)
	}
	if _, _, _, err := sub.resolveProvider("", sub.taskEffort); err != nil {
		t.Fatalf("supported inherited effort should resolve: %v", err)
	}
	if len(resolver.selections) != 1 || resolver.selections[0].Effort == nil || *resolver.selections[0].Effort != "max" {
		t.Fatalf("provider selections = %+v, want max override", resolver.selections)
	}
	model, effort := sub.identity("", sub.taskEffort)
	if model != "custom/supported" || effort != "max" {
		t.Fatalf("effective identity = %q/%q, want custom/supported/max", model, effort)
	}
	profile := skillProfile(cfg, sub.inheritedEffort)(skill.Skill{Name: "review", RunAs: skill.RunSubagent})
	if profile == nil || profile.Effort != "max" {
		t.Fatalf("skill profile = %+v, want inherited max", profile)
	}
}

func TestSubagentEffortOverridesRemainStrict(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.SubagentEfforts = map[string]string{"task": "max"}
	cfg.Providers = []config.ProviderEntry{{
		Name: "custom", Kind: "openai", Models: []string{"fallback"}, Default: "fallback",
		SupportedEfforts: []string{"low", "high"}, DefaultEffort: "high",
	}}
	entry, ok := cfg.ResolveModel("custom/fallback")
	if !ok {
		t.Fatal("custom/fallback should resolve")
	}
	resolver := &effortSelectionResolver{rejectEffort: true}
	sub := newSubagentConfig(Options{}, cfg, entry, "custom/fallback", resolver, netclient.ProxySpec{}, nil)

	if sub.taskEffort != "max" {
		t.Fatalf("task-specific effort = %q, want max", sub.taskEffort)
	}
	if _, _, _, err := sub.resolveProvider("", sub.taskEffort); err == nil {
		t.Fatal("task-specific unsupported effort should remain strict")
	}
	if cfg.Agent.SubagentEfforts["task"] != "max" {
		t.Fatalf("task-specific persistent effort changed: %q", cfg.Agent.SubagentEfforts["task"])
	}
}

func TestExplicitSubagentModelAndGlobalEffortRemainStrict(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.SubagentModel = "custom/fallback"
	cfg.Agent.SubagentEffort = "max"
	cfg.Providers = []config.ProviderEntry{{
		Name: "custom", Kind: "openai", Models: []string{"fallback"}, Default: "fallback",
		SupportedEfforts: []string{"low", "high"}, DefaultEffort: "high",
	}}
	entry, ok := cfg.ResolveModel("custom/fallback")
	if !ok {
		t.Fatal("custom/fallback should resolve")
	}
	resolver := &effortSelectionResolver{rejectEffort: true}
	sub := newSubagentConfig(Options{}, cfg, entry, "custom/fallback", resolver, netclient.ProxySpec{}, nil)

	if sub.taskModel != "custom/fallback" || sub.taskEffort != "max" || sub.inheritedEffortDropped {
		t.Fatalf("explicit pair = %q/%q/dropped=%v, want custom/fallback/max/false", sub.taskModel, sub.taskEffort, sub.inheritedEffortDropped)
	}
	if _, _, _, err := sub.resolveProvider(sub.taskModel, sub.taskEffort); err == nil {
		t.Fatal("explicit model/effort pair should remain strict")
	}
}

func TestSkillEffortUsesResolvedInheritedDefault(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.SubagentEffort = "max"
	cfg.Providers = []config.ProviderEntry{{
		Name: "custom", Kind: "openai", Models: []string{"fallback"}, Default: "fallback",
		SupportedEfforts: []string{"low", "high"}, DefaultEffort: "high",
	}}
	entry, ok := cfg.ResolveModel("custom/fallback")
	if !ok {
		t.Fatal("custom/fallback should resolve")
	}
	resolver := &effortSelectionResolver{rejectEffort: true}
	sub := newSubagentConfig(Options{}, cfg, entry, "custom/fallback", resolver, netclient.ProxySpec{}, nil)
	runner := &skillSubagents{
		cfg:             cfg,
		provider:        nil,
		entry:           entry,
		inheritedEffort: sub.inheritedEffort,
		resolveProvider: sub.resolveProvider,
	}

	_, _, _, modelRef, effortRef, err := runner.resolveModel(skill.Skill{Name: "review", RunAs: skill.RunSubagent})
	if err != nil {
		t.Fatalf("skill without own effort should use provider default: %v", err)
	}
	if modelRef != "" || effortRef != "" || len(resolver.selections) != 0 {
		t.Fatalf("skill resolution = %q/%q, selections=%+v; want no override", modelRef, effortRef, resolver.selections)
	}

	_, _, _, _, _, err = runner.resolveModel(skill.Skill{Name: "review", RunAs: skill.RunSubagent, Effort: "max"})
	if err == nil {
		t.Fatal("explicit skill effort should remain strict")
	}
	if got := skillProfile(cfg, sub.inheritedEffort)(skill.Skill{Name: "review", RunAs: skill.RunSubagent}); got != nil {
		t.Fatalf("fallback skill profile = %+v, want no stale inherited effort", got)
	}
}
