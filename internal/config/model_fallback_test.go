package config

import (
	"strings"
	"testing"
)

func testModelFallbackConfig(t *testing.T) *Config {
	t.Helper()
	t.Setenv("REASONIX_TEST_KEY", "sk-test")
	t.Setenv("REASONIX_TEST_EMPTY", "")

	c := Default()
	c.DefaultModel = "prov-a"
	c.Providers = []ProviderEntry{
		{Name: "prov-a", Kind: "openai", BaseURL: "https://a.example.com", Model: "model-a1", Models: []string{"model-a1", "model-a2"}, APIKeyEnv: "REASONIX_TEST_KEY"},
		{Name: "prov-b", Kind: "openai", BaseURL: "https://b.example.com", Model: "model-b1", Models: []string{"model-b1", "model-b2"}, APIKeyEnv: "REASONIX_TEST_KEY"},
		{Name: "prov-nokey", Kind: "openai", BaseURL: "https://nk.example.com", Model: "model-nk", APIKeyEnv: "REASONIX_TEST_EMPTY"},
	}
	return c
}

func TestResolveModelWithFallback(t *testing.T) {
	c := testModelFallbackConfig(t)

	cases := []struct {
		name         string
		ref          string
		wantResolved string
		wantFallback bool
		wantOK       bool
	}{
		{"direct provider model", "prov-a/model-a2", "prov-a/model-a2", false, true},
		{"provider name", "prov-b", "prov-b/model-b1", false, true},
		{"bare model", "model-a1", "prov-a/model-a1", false, true},
		{"empty falls back", "", "prov-a/model-a1", true, true},
		{"stale falls back", "deleted/model", "prov-a/model-a1", true, true},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, fallback, ok := c.ResolveModelWithFallback(tt.ref)
			if got != tt.wantResolved || fallback != tt.wantFallback || ok != tt.wantOK {
				t.Fatalf("ResolveModelWithFallback(%q) = (%q, %v, %v), want (%q, %v, %v)", tt.ref, got, fallback, ok, tt.wantResolved, tt.wantFallback, tt.wantOK)
			}
		})
	}

	c.Providers = nil
	if got, fallback, ok := c.ResolveModelWithFallback("deleted/model"); got != "" || fallback || ok {
		t.Fatalf("no providers fallback = (%q, %v, %v), want empty false false", got, fallback, ok)
	}
}

func TestResolveModelWithFallbackSkipsKeylessProvider(t *testing.T) {
	c := testModelFallbackConfig(t)
	// Make the first provider keyless. A fallback must skip it and pick the next
	// configured provider, rather than booting a tab onto a provider with no API
	// key (which just fails on first use).
	c.Providers[0].APIKeyEnv = "REASONIX_TEST_EMPTY"

	got, fallback, ok := c.ResolveModelWithFallback("")
	if !ok || !fallback {
		t.Fatalf("ResolveModelWithFallback(\"\") = (%q, %v, %v), want a fallback", got, fallback, ok)
	}
	if got != "prov-b/model-b1" {
		t.Errorf("fallback = %q, want prov-b/model-b1 (prov-a is keyless and must be skipped)", got)
	}
}

// TestResolveModelWithFallbackHonorsDefaultModel verifies that when the ref is
// stale or empty, the function tries c.DefaultModel before iterating providers
// in order. Without this, the first provider (deepseek, by default) always wins
// even when the user has configured a different default_model (#3801).
func TestResolveModelWithFallbackHonorsDefaultModel(t *testing.T) {
	c := testModelFallbackConfig(t)
	// Set DefaultModel to prov-b (not the first provider). An empty/stale ref
	// should fall back to prov-b, not prov-a.
	c.DefaultModel = "prov-b"

	// Empty ref → must use DefaultModel = prov-b
	got, fallback, ok := c.ResolveModelWithFallback("")
	if !ok || !fallback {
		t.Fatalf("ResolveModelWithFallback(\"\") = (%q, %v, %v), want fallback to prov-b", got, fallback, ok)
	}
	if got != "prov-b/model-b1" {
		t.Errorf("empty ref fallback = %q, want prov-b/model-b1 (DefaultModel)", got)
	}

	// Stale ref → same: must use DefaultModel
	got, fallback, ok = c.ResolveModelWithFallback("deleted/model")
	if !ok || !fallback {
		t.Fatalf("ResolveModelWithFallback(\"deleted/model\") = (%q, %v, %v), want fallback", got, fallback, ok)
	}
	if got != "prov-b/model-b1" {
		t.Errorf("stale ref fallback = %q, want prov-b/model-b1 (DefaultModel)", got)
	}

	// DefaultModel pointing to a keyless provider must be skipped
	c.Providers[1].APIKeyEnv = "REASONIX_TEST_EMPTY" // prov-b, the DefaultModel
	got, fallback, ok = c.ResolveModelWithFallback("stale/ref")
	if !ok || !fallback {
		t.Fatalf("keyless DefaultModel fallback = (%q, %v, %v), want next configured", got, fallback, ok)
	}
	if got != "prov-a/model-a1" {
		t.Errorf("keyless DefaultModel fallback = %q, want prov-a/model-a1 (only configured left)", got)
	}
}

func TestModelRefsProvider(t *testing.T) {
	if !ModelRefsProvider("deepseek-flash", "deepseek-flash") {
		t.Fatal("bare provider ref should match provider")
	}
	if !ModelRefsProvider("deepseek-flash/deepseek-v4-flash", "deepseek-flash") {
		t.Fatal("provider/model ref should match provider")
	}
	if ModelRefsProvider("other/model", "deepseek-flash") {
		t.Fatal("different provider should not match")
	}
	if ModelRefsProvider("", "deepseek-flash") {
		t.Fatal("empty ref should not match")
	}
}

func TestRemoveProviderMigratesDanglingRefs(t *testing.T) {
	c := testModelFallbackConfig(t)
	c.DefaultModel = "model-a2"
	c.Agent.PlannerModel = "prov-a"
	c.Agent.SubagentModel = "prov-a/model-a1"
	c.Agent.SubagentModels = map[string]string{
		"review":  "prov-a/model-a2",
		"bare":    "model-a1",
		"explore": "prov-b/model-b1",
	}

	if err := c.RemoveProvider("prov-a"); err != nil {
		t.Fatalf("RemoveProvider: %v", err)
	}
	if _, ok := c.Provider("prov-a"); ok {
		t.Fatal("provider should be removed")
	}
	if c.DefaultModel != "prov-b" {
		t.Fatalf("default_model = %q, want prov-b", c.DefaultModel)
	}
	if c.Agent.PlannerModel != "prov-b" {
		t.Fatalf("planner_model = %q, want prov-b", c.Agent.PlannerModel)
	}
	if c.Agent.SubagentModel != "prov-b" {
		t.Fatalf("subagent_model = %q, want prov-b", c.Agent.SubagentModel)
	}
	if c.Agent.SubagentModels["review"] != "prov-b" {
		t.Fatalf("subagent_models.review = %q, want prov-b", c.Agent.SubagentModels["review"])
	}
	if c.Agent.SubagentModels["bare"] != "prov-b" {
		t.Fatalf("subagent_models.bare = %q, want prov-b", c.Agent.SubagentModels["bare"])
	}
	if c.Agent.SubagentModels["explore"] != "prov-b/model-b1" {
		t.Fatalf("unaffected subagent model changed to %q", c.Agent.SubagentModels["explore"])
	}
}

func TestRemoveProviderBlocksDefaultWithoutFallback(t *testing.T) {
	c := testModelFallbackConfig(t)
	c.DefaultModel = "prov-a/model-a1"
	c.Providers[1].APIKeyEnv = "REASONIX_TEST_EMPTY"

	err := c.RemoveProvider("prov-a")
	if err == nil {
		t.Fatal("expected removing default provider without fallback to fail")
	}
	if !strings.Contains(err.Error(), "default_model") {
		t.Fatalf("error = %q, want default_model mention", err)
	}
	if _, ok := c.Provider("prov-a"); !ok {
		t.Fatal("provider should remain after failed removal")
	}
}

func TestRemoveProviderClearsOptionalRefsWithoutFallback(t *testing.T) {
	c := testModelFallbackConfig(t)
	c.DefaultModel = "prov-b"
	c.Agent.PlannerModel = "prov-a/model-a1"
	c.Agent.SubagentModel = "prov-a"
	c.Agent.SubagentModels = map[string]string{"review": "prov-a/model-a2"}
	c.Providers[1].APIKeyEnv = "REASONIX_TEST_EMPTY"

	if err := c.RemoveProvider("prov-a"); err != nil {
		t.Fatalf("RemoveProvider: %v", err)
	}
	if c.Agent.PlannerModel != "" {
		t.Fatalf("planner_model = %q, want cleared", c.Agent.PlannerModel)
	}
	if c.Agent.SubagentModel != "" {
		t.Fatalf("subagent_model = %q, want cleared", c.Agent.SubagentModel)
	}
	if _, ok := c.Agent.SubagentModels["review"]; ok {
		t.Fatal("subagent_models.review should be removed")
	}
}
