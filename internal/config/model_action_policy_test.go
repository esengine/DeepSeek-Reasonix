package config

import (
	"os"
	"path/filepath"
	"testing"
)

func loadProviderEntryForActionPolicy(t *testing.T, providerBody string) *ProviderEntry {
	t.Helper()
	dir := t.TempDir()
	body := "default_model = \"test-model\"\n\n[[providers]]\nname = \"test-model\"\nkind = \"openai\"\nmodel = \"x\"\n" + providerBody + "\n"
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	c, err := LoadForRootReadOnly(dir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	e, ok := c.ResolveModel("test-model")
	if !ok {
		t.Fatal("ResolveModel did not resolve the configured provider")
	}
	return e
}

func TestActionPolicyPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{name: "unset", want: false},
		{
			name: "model override on",
			body: `model_overrides = { "x" = { action_policy = true } }`,
			want: true,
		},
		{
			name: "model override off",
			body: `model_overrides = { "x" = { action_policy = false } }`,
			want: false,
		},
		{
			name: "override for another model does not apply",
			body: `model_overrides = { "other" = { action_policy = true } }`,
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := loadProviderEntryForActionPolicy(t, tc.body)
			if got := AppliesModelActionPolicy(e); got != tc.want {
				t.Fatalf("AppliesModelActionPolicy = %v, want %v", got, tc.want)
			}
		})
	}
}

// normalizedModelOverrides drops overrides it considers empty. An override that
// only carries action_policy must survive that pass, or the key silently does
// nothing when it is the sole entry.
func TestActionPolicyOnlyOverrideSurvivesLoad(t *testing.T) {
	e := loadProviderEntryForActionPolicy(t, `model_overrides = { "x" = { action_policy = true } }`)
	if !AppliesModelActionPolicy(e) {
		t.Fatal("an action_policy-only model override was dropped during load")
	}
}

func TestApplyModelActionPolicyLeavesDefaultPromptUntouched(t *testing.T) {
	const base = "BASE PROMPT"
	if got := ApplyModelActionPolicy(base, &ProviderEntry{}); got != base {
		t.Fatalf("default entry changed the prompt: %q", got)
	}
	if got := ApplyModelActionPolicy(base, nil); got != base {
		t.Fatalf("nil entry changed the prompt: %q", got)
	}
	opted := true
	e := &ProviderEntry{Model: "x", ModelOverrides: map[string]ProviderModelOverride{"x": {ActionPolicy: &opted}}}
	got := ApplyModelActionPolicy(base, e)
	if got != base+"\n\n"+ModelActionPolicy {
		t.Fatalf("opt-in did not append exactly one paragraph: %q", got)
	}
}

// actionPolicyFor reads the tri-state opt-in for one model of one provider.
func actionPolicyFor(t *testing.T, c *Config, provider, model string) *bool {
	t.Helper()
	p, ok := c.Provider(provider)
	if !ok {
		t.Fatalf("provider %q missing from config", provider)
	}
	ov, ok := p.modelOverrideForModel(model)
	if !ok {
		t.Fatalf("provider %q has no model override for %q: %+v", provider, model, p.ModelOverrides)
	}
	return ov.ActionPolicy
}

func wantActionPolicy(t *testing.T, got *bool, want bool, where string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: action_policy was dropped, want explicit %t", where, want)
	}
	if *got != want {
		t.Fatalf("%s: action_policy = %t, want %t", where, *got, want)
	}
}

// Canonicalizing the legacy official DeepSeek providers folds each old entry's
// model overrides onto the new one. An operator's explicit opt-in — and an
// explicit opt-out, which is a decision too — has to survive that migration and
// the render/load round trip that persists it.
func TestLegacyDeepSeekCanonicalizationKeepsExplicitActionPolicy(t *testing.T) {
	on, off := true, false
	c := Default()
	if _, ok := c.Provider("deepseek"); ok {
		t.Fatal("the default config already has a canonical provider; nothing would be migrated")
	}
	// Opt in on one legacy provider and explicitly out on the other, the shape a
	// user config reaches this migration in.
	for _, tc := range []struct {
		provider, model string
		policy          *bool
	}{
		{provider: "deepseek-flash", model: "deepseek-v4-flash", policy: &on},
		{provider: "deepseek-pro", model: "deepseek-v4-pro", policy: &off},
	} {
		p, ok := c.Provider(tc.provider)
		if !ok {
			t.Fatalf("default config has no legacy %q provider to migrate", tc.provider)
		}
		if p.ModelOverrides == nil {
			p.ModelOverrides = map[string]ProviderModelOverride{}
		}
		ov := p.ModelOverrides[tc.model]
		ov.ActionPolicy = tc.policy
		p.ModelOverrides[tc.model] = ov
	}
	if got := len(officialLegacyDeepSeekProviders(c)); got != 2 {
		t.Fatalf("want both legacy providers eligible for canonicalization, got %d", got)
	}

	ensureDeepSeekOfficialProvider(c)
	wantActionPolicy(t, actionPolicyFor(t, c, "deepseek", "deepseek-v4-flash"), true, "after migration")
	wantActionPolicy(t, actionPolicyFor(t, c, "deepseek", "deepseek-v4-pro"), false, "after migration")

	// The migrated opt-in must actually reach the prompt, not merely survive as
	// a struct field the resolver never consults.
	canonical, _ := c.Provider("deepseek")
	opted := cloneProviderEntry(*canonical)
	opted.Model = "deepseek-v4-flash"
	if !AppliesModelActionPolicy(&opted) {
		t.Fatal("migrated opt-in did not reach AppliesModelActionPolicy")
	}
	declined := cloneProviderEntry(*canonical)
	declined.Model = "deepseek-v4-pro"
	if AppliesModelActionPolicy(&declined) {
		t.Fatal("migrated opt-out enabled the policy")
	}

	// Render and load again: the persisted form has to carry both states.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(RenderTOML(c)), 0o644); err != nil {
		t.Fatalf("write rendered config: %v", err)
	}
	back, err := LoadForRootReadOnly(dir)
	if err != nil {
		t.Fatalf("reload rendered config: %v", err)
	}
	wantActionPolicy(t, actionPolicyFor(t, back, "deepseek", "deepseek-v4-flash"), true, "after round trip")
	wantActionPolicy(t, actionPolicyFor(t, back, "deepseek", "deepseek-v4-pro"), false, "after round trip")
}

// cloneModelOverrideMap hands out copies so two configs never share pointer
// state: mutating a clone's opt-in must not reach through to the original.
func TestCloneModelOverrideMapCopiesActionPolicy(t *testing.T) {
	opted := true
	in := map[string]ProviderModelOverride{"x": {ActionPolicy: &opted}}
	out := cloneModelOverrideMap(in)
	if out["x"].ActionPolicy == nil {
		t.Fatal("clone dropped action_policy")
	}
	if out["x"].ActionPolicy == in["x"].ActionPolicy {
		t.Fatal("clone shares the action_policy pointer with the source")
	}
	*out["x"].ActionPolicy = false
	if !*in["x"].ActionPolicy {
		t.Fatal("mutating the clone reached through to the source")
	}
}
