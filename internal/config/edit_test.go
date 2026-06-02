package config

import (
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestSetDefaultModel(t *testing.T) {
	c := Default()
	if err := c.SetDefaultModel("mimo-pro"); err != nil {
		t.Fatalf("set valid default: %v", err)
	}
	if c.DefaultModel != "mimo-pro" {
		t.Errorf("default = %q, want mimo-pro", c.DefaultModel)
	}
	if err := c.SetDefaultModel("nope"); err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestSetPlannerModel(t *testing.T) {
	c := Default()
	if err := c.SetPlannerModel("deepseek-pro"); err != nil {
		t.Fatalf("set planner: %v", err)
	}
	if c.Agent.PlannerModel != "deepseek-pro" {
		t.Errorf("planner = %q", c.Agent.PlannerModel)
	}
	if err := c.SetPlannerModel(""); err != nil || c.Agent.PlannerModel != "" {
		t.Errorf("clearing planner failed: err=%v planner=%q", err, c.Agent.PlannerModel)
	}
	if err := c.SetPlannerModel("ghost"); err == nil {
		t.Error("expected error for unknown planner")
	}
}

func TestUpsertProvider(t *testing.T) {
	c := Default()
	n := len(c.Providers)

	// Add a new one.
	if err := c.UpsertProvider(ProviderEntry{Name: "local", Kind: "openai", BaseURL: "http://localhost:1234/v1", Model: "x"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(c.Providers) != n+1 {
		t.Fatalf("provider count = %d, want %d", len(c.Providers), n+1)
	}

	// Replace it in place (no growth, position preserved).
	if err := c.UpsertProvider(ProviderEntry{Name: "local", Kind: "openai", BaseURL: "http://localhost:9999/v1", Model: "y"}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if len(c.Providers) != n+1 {
		t.Errorf("replace grew the list to %d", len(c.Providers))
	}
	got, _ := c.Provider("local")
	if got.BaseURL != "http://localhost:9999/v1" || got.Model != "y" {
		t.Errorf("replace didn't apply: %+v", got)
	}

	// Missing required fields error.
	for _, bad := range []ProviderEntry{
		{Kind: "openai", BaseURL: "u", Model: "m"}, // no name
		{Name: "a", BaseURL: "u", Model: "m"},      // no kind
		{Name: "a", Kind: "openai", Model: "m"},    // no base_url
		{Name: "a", Kind: "openai", BaseURL: "u"},  // no model
	} {
		if err := c.UpsertProvider(bad); err == nil {
			t.Errorf("expected validation error for %+v", bad)
		}
	}
}

func TestSetProviderEffort(t *testing.T) {
	c := Default()
	if err := c.SetProviderEffort("deepseek-flash", "MAX"); err != nil {
		t.Fatalf("SetProviderEffort: %v", err)
	}
	p, _ := c.Provider("deepseek-flash")
	if p.Effort != "max" {
		t.Fatalf("effort = %q, want max", p.Effort)
	}
	if err := c.SetProviderEffort("missing", "high"); err == nil {
		t.Fatal("SetProviderEffort should reject unknown provider")
	}
}

func TestResolveModelPreservesProviderEffort(t *testing.T) {
	c := Default()
	c.Providers = append(c.Providers, ProviderEntry{
		Name:      "deepseek",
		Kind:      "openai",
		BaseURL:   "https://api.deepseek.com",
		Model:     "deepseek-v4-flash",
		Models:    []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		Default:   "deepseek-v4-flash",
		APIKeyEnv: "DEEPSEEK_API_KEY",
		Effort:    "max",
	})
	e, ok := c.ResolveModel("deepseek/deepseek-v4-pro")
	if !ok {
		t.Fatal("ResolveModel did not find deepseek/deepseek-v4-pro")
	}
	if e.Name != "deepseek" || e.Model != "deepseek-v4-pro" || e.Effort != "max" {
		t.Fatalf("resolved entry = %+v, want provider deepseek model deepseek-v4-pro effort max", e)
	}
}

func TestRemoveProvider(t *testing.T) {
	c := Default()
	c.Agent.PlannerModel = "deepseek-pro"

	// Cannot remove the default model.
	if err := c.RemoveProvider(c.DefaultModel); err == nil {
		t.Error("expected error removing the default model")
	}
	// Removing the planner provider clears planner_model.
	if err := c.RemoveProvider("deepseek-pro"); err != nil {
		t.Fatalf("remove planner provider: %v", err)
	}
	if c.Agent.PlannerModel != "" {
		t.Errorf("planner should be cleared, got %q", c.Agent.PlannerModel)
	}
	if _, ok := c.Provider("deepseek-pro"); ok {
		t.Error("provider not actually removed")
	}
	// Unknown name errors.
	if err := c.RemoveProvider("ghost"); err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestPermissionMutators(t *testing.T) {
	c := Default()

	if err := c.SetPermissionMode("DENY"); err != nil || c.Permissions.Mode != "deny" {
		t.Errorf("set mode: err=%v mode=%q", err, c.Permissions.Mode)
	}
	if err := c.SetPermissionMode("nonsense"); err == nil {
		t.Error("expected error for bad mode")
	}

	if err := c.AddPermissionRule("deny", "bash(rm -rf*)"); err != nil {
		t.Fatalf("add deny: %v", err)
	}
	// Duplicate is a no-op, not an error or a second entry.
	if err := c.AddPermissionRule("deny", "bash(rm -rf*)"); err != nil {
		t.Fatalf("dup add: %v", err)
	}
	if len(c.Permissions.Deny) != 1 {
		t.Errorf("deny list = %v, want one entry", c.Permissions.Deny)
	}
	// Invalid rule and unknown list both error.
	if err := c.AddPermissionRule("deny", "  "); err == nil {
		t.Error("expected error for empty rule")
	}
	if err := c.AddPermissionRule("nope", "read_file"); err == nil {
		t.Error("expected error for unknown list")
	}

	removed, err := c.RemovePermissionRule("deny", "bash(rm -rf*)")
	if err != nil || !removed {
		t.Errorf("remove: removed=%v err=%v", removed, err)
	}
	if removed, _ := c.RemovePermissionRule("deny", "absent"); removed {
		t.Error("removing absent rule should report false")
	}
}

func TestPluginMutators(t *testing.T) {
	c := Default()

	if err := c.UpsertPlugin(PluginEntry{Name: "ex", Command: "reasonix-plugin-example"}); err != nil {
		t.Fatalf("add stdio: %v", err)
	}
	if err := c.UpsertPlugin(PluginEntry{Name: "stripe", Type: "http", URL: "https://mcp.stripe.com"}); err != nil {
		t.Fatalf("add http: %v", err)
	}
	if len(c.Plugins) != 2 {
		t.Fatalf("plugin count = %d, want 2", len(c.Plugins))
	}

	// Transport validation: stdio needs command, http needs url.
	if err := c.UpsertPlugin(PluginEntry{Name: "bad"}); err == nil {
		t.Error("stdio without command should error")
	}
	if err := c.UpsertPlugin(PluginEntry{Name: "bad", Type: "http"}); err == nil {
		t.Error("http without url should error")
	}
	if err := c.UpsertPlugin(PluginEntry{Name: "bad", Type: "carrier-pigeon", Command: "x"}); err == nil {
		t.Error("unknown transport should error")
	}

	// Replace in place.
	if err := c.UpsertPlugin(PluginEntry{Name: "ex", Command: "other-cmd"}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if len(c.Plugins) != 2 {
		t.Errorf("replace grew plugins to %d", len(c.Plugins))
	}

	if !c.RemovePlugin("ex") {
		t.Error("remove should report true")
	}
	if c.RemovePlugin("ex") {
		t.Error("second remove should report false")
	}
}

func TestAutoStartPlugins(t *testing.T) {
	c := Default()
	off := false
	on := true
	c.Plugins = []PluginEntry{
		{Name: "implicit", Command: "implicit-bin"},
		{Name: "disabled", Command: "disabled-bin", AutoStart: &off},
		{Name: "enabled", Command: "enabled-bin", AutoStart: &on},
	}
	got := c.AutoStartPlugins()
	if len(got) != 2 || got[0].Name != "implicit" || got[1].Name != "enabled" {
		t.Fatalf("AutoStartPlugins = %+v, want implicit + enabled", got)
	}
}

// TestSaveToRoundTrips stages several mutations, persists atomically, and
// re-decodes the file to confirm the changes survived a write/read cycle.
func TestSaveToRoundTrips(t *testing.T) {
	c := Default()
	if err := c.SetDefaultModel("mimo-pro"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetPlannerModel("deepseek-pro"); err != nil {
		t.Fatal(err)
	}
	if err := c.UpsertProvider(ProviderEntry{Name: "local", Kind: "openai", BaseURL: "http://localhost:1234/v1", Model: "llama"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetPermissionMode("deny"); err != nil {
		t.Fatal(err)
	}
	if err := c.AddPermissionRule("allow", "bash(go test*)"); err != nil {
		t.Fatal(err)
	}
	autoStart := false
	if err := c.UpsertPlugin(PluginEntry{Name: "stripe", Type: "http", URL: "https://mcp.stripe.com", AutoStart: &autoStart}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "nested", "reasonix.toml")
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	var got Config
	if _, err := toml.DecodeFile(path, &got); err != nil {
		t.Fatalf("saved file does not parse: %v", err)
	}
	if got.DefaultModel != "mimo-pro" {
		t.Errorf("default_model = %q", got.DefaultModel)
	}
	if got.Agent.PlannerModel != "deepseek-pro" {
		t.Errorf("planner_model = %q", got.Agent.PlannerModel)
	}
	if _, ok := got.Provider("local"); !ok {
		t.Error("added provider 'local' missing after round-trip")
	}
	if got.Permissions.Mode != "deny" {
		t.Errorf("mode = %q", got.Permissions.Mode)
	}
	if len(got.Permissions.Allow) != 1 || got.Permissions.Allow[0] != "bash(go test*)" {
		t.Errorf("allow list = %v", got.Permissions.Allow)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Name != "stripe" {
		t.Errorf("plugins = %+v", got.Plugins)
	}
	if got.Plugins[0].AutoStart == nil || *got.Plugins[0].AutoStart {
		t.Errorf("auto_start should round-trip false, got %+v", got.Plugins[0].AutoStart)
	}
}
