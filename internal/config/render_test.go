package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

// TestRenderTOMLRoundTrips ensures the annotated TOML we emit parses back into
// an equivalent config — i.e. the wizard never writes a file it can't read.
func TestRenderTOMLRoundTrips(t *testing.T) {
	orig := Default()
	orig.DefaultModel = "mimo-pro"
	orig.Language = "zh"
	orig.Agent.AutoPlanClassifier = "deepseek-flash"
	orig.Agent.SubagentModel = "mimo-pro"
	orig.Agent.SubagentModels = map[string]string{"review": "deepseek-pro"}
	orig.Permissions = PermissionsConfig{
		Mode:  "deny",
		Deny:  []string{"bash(rm -rf*)"},
		Allow: []string{"bash(go test*)", "read_file"},
	}
	orig.Plugins = []PluginEntry{
		{Name: "example", Command: "reasonix-plugin-example"},
		{Name: "stripe", Type: "http", URL: "https://mcp.stripe.com", Headers: map[string]string{"Authorization": "Bearer x"}, AutoStart: boolPtr(false)},
	}
	mm, _ := orig.Provider("mimo-pro")
	mm.BaseURL = "http://localhost:8000/v1"
	ds, _ := orig.Provider("deepseek-flash")
	ds.Effort = "max"

	rendered := RenderTOML(orig)

	var got Config
	if _, err := toml.Decode(rendered, &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v\n---\n%s", err, rendered)
	}

	if got.DefaultModel != "mimo-pro" {
		t.Errorf("default_model = %q, want mimo-pro", got.DefaultModel)
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
	if got.Agent.AutoPlanClassifier != "deepseek-flash" {
		t.Errorf("auto_plan_classifier = %q, want deepseek-flash", got.Agent.AutoPlanClassifier)
	}
	if got.Agent.SystemPrompt != orig.Agent.SystemPrompt {
		t.Errorf("system_prompt mismatch:\n got %q\nwant %q", got.Agent.SystemPrompt, orig.Agent.SystemPrompt)
	}
	if got.Agent.SubagentModel != "mimo-pro" {
		t.Errorf("subagent_model = %q, want mimo-pro", got.Agent.SubagentModel)
	}
	if got.Agent.SubagentModels["review"] != "deepseek-pro" {
		t.Errorf("subagent_models.review = %q, want deepseek-pro", got.Agent.SubagentModels["review"])
	}
	if g, _ := got.Provider("mimo-pro"); g == nil || g.BaseURL != "http://localhost:8000/v1" {
		t.Errorf("mimo-pro base_url not preserved: %+v", g)
	}
	if g, _ := got.Provider("deepseek-flash"); g == nil || g.Effort != "max" {
		t.Errorf("deepseek-flash effort not preserved: %+v", g)
	}
	if len(got.Providers) != len(orig.Providers) {
		t.Errorf("providers count = %d, want %d", len(got.Providers), len(orig.Providers))
	}
	if got.Permissions.Mode != "deny" {
		t.Errorf("permissions.mode = %q, want deny", got.Permissions.Mode)
	}
	if len(got.Permissions.Deny) != 1 || got.Permissions.Deny[0] != "bash(rm -rf*)" {
		t.Errorf("permissions.deny = %v, want [bash(rm -rf*)]", got.Permissions.Deny)
	}
	if len(got.Permissions.Allow) != 2 {
		t.Errorf("permissions.allow = %v, want 2 entries", got.Permissions.Allow)
	}
	if len(got.Plugins) != 2 {
		t.Fatalf("plugins count = %d, want 2", len(got.Plugins))
	}
	stripe := got.Plugins[1]
	if stripe.Name != "stripe" || stripe.Type != "http" || stripe.URL != "https://mcp.stripe.com" {
		t.Errorf("http plugin not preserved: %+v", stripe)
	}
	if stripe.Headers["Authorization"] != "Bearer x" {
		t.Errorf("plugin headers not preserved: %v", stripe.Headers)
	}
	if stripe.AutoStart == nil || *stripe.AutoStart {
		t.Errorf("auto_start should render and parse as false, got %+v", stripe.AutoStart)
	}
}

func boolPtr(v bool) *bool { return &v }
