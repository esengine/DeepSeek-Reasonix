package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// TestRenderTOMLRoundTrips ensures the annotated TOML we emit parses back into
// an equivalent config — i.e. the wizard never writes a file it can't read.
func TestRenderTOMLRoundTrips(t *testing.T) {
	orig := Default()
	orig.DefaultModel = "deepseek"
	orig.Language = "zh"
	orig.Agent.AutoPlanClassifier = "deepseek"
	orig.Agent.SubagentModel = "deepseek"
	orig.Agent.SubagentModels = map[string]string{"review": "deepseek"}
	orig.Permissions = PermissionsConfig{
		Mode:  "deny",
		Deny:  []string{"bash(rm -rf*)"},
		Allow: []string{"bash(go test*)", "read_file"},
	}
	orig.Plugins = []PluginEntry{
		{Name: "example", Command: "reasonix-plugin-example"},
		{Name: "stripe", Type: "http", URL: "https://mcp.stripe.com", Headers: map[string]string{"Authorization": "Bearer x"}, AutoStart: boolPtr(false)},
	}
	// Modify the deepseek provider's base_url for round-trip testing.
	ds, _ := orig.Provider("deepseek")
	ds.BaseURL = "http://localhost:8000/v1"

	rendered := RenderTOML(orig)

	var got Config
	if _, err := toml.Decode(rendered, &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v\n---\n%s", err, rendered)
	}

	if got.DefaultModel != "deepseek" {
		t.Errorf("default_model = %q, want deepseek", got.DefaultModel)
	}
	if got.Language != "zh" {
		t.Errorf("language = %q, want zh", got.Language)
	}
	if got.Agent.MaxSteps != orig.Agent.MaxSteps {
		t.Errorf("max_steps = %d, want %d", got.Agent.MaxSteps, orig.Agent.MaxSteps)
	}
	if got.Agent.Temperature != orig.Agent.Temperature {
		t.Errorf("temperature = %v, want %v", got.Agent.Temperature, orig.Agent.Temperature)
	}
	if got.Agent.AutoPlan != "ask" {
		t.Errorf("auto_plan = %q, want ask", got.Agent.AutoPlan)
	}
	if got.Agent.AutoPlanClassifier != "deepseek" {
		t.Errorf("auto_plan_classifier = %q", got.Agent.AutoPlanClassifier)
	}
	if got.Agent.SubagentModel != "deepseek" {
		t.Errorf("subagent_model = %q", got.Agent.SubagentModel)
	}
	if got.Agent.SubagentModels["review"] != "deepseek" {
		t.Errorf("subagent_models[review] = %q", got.Agent.SubagentModels["review"])
	}

	// Provider round-trip.
	if len(got.Providers) != len(orig.Providers) {
		t.Fatalf("provider count = %d, want %d", len(got.Providers), len(orig.Providers))
	}
	gotDS, ok := got.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider missing")
	}
	if gotDS.BaseURL != "http://localhost:8000/v1" {
		t.Errorf("deepseek base_url = %q", gotDS.BaseURL)
	}
	if len(gotDS.Models) != 2 {
		t.Errorf("deepseek models count = %d, want 2", len(gotDS.Models))
	}

	// Permissions round-trip.
	if got.Permissions.Mode != "deny" {
		t.Errorf("mode = %q", got.Permissions.Mode)
	}
	if len(got.Permissions.Deny) != 1 || got.Permissions.Deny[0] != "bash(rm -rf*)" {
		t.Errorf("deny = %v", got.Permissions.Deny)
	}
	if len(got.Permissions.Allow) != 2 {
		t.Errorf("allow count = %d", len(got.Permissions.Allow))
	}

	// Plugin round-trip.
	if len(got.Plugins) != 2 {
		t.Fatalf("plugin count = %d", len(got.Plugins))
	}
	if got.Plugins[1].Name != "stripe" {
		t.Errorf("plugin[1] = %q", got.Plugins[1].Name)
	}
	if got.Plugins[1].AutoStart == nil || *got.Plugins[1].AutoStart {
		t.Errorf("auto_start = %v", got.Plugins[1].AutoStart)
	}
	if got.Plugins[1].Headers["Authorization"] != "Bearer x" {
		t.Errorf("stripe header = %v", got.Plugins[1].Headers)
	}
}

func boolPtr(b bool) *bool { return &b }
