package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedModelSnapshotPreservesProjectProviderAndAssignments(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	t.Setenv("PROJECT_MODEL_KEY", "project-secret")
	if _, err := SetCredential("PROJECT_MODEL_KEY", "project-secret"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	project := `[agent]
planner_model = "local/m"
vision_model = "global/g"
subagent_effort = "high"
[agent.subagent_models]
review = "global/g"
[[providers]]
name = "local"
kind = "openai"
base_url = "https://project.invalid/v1"
model = "m"
api_key_env = "PROJECT_MODEL_KEY"
`
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(project), 0600); err != nil {
		t.Fatal(err)
	}
	bundle := &ModelRuntimeSettings{Revision: "revision", ProxyURL: "http://127.0.0.1:9876", Providers: []ProviderEntry{
		{Name: "local-alias", Kind: "anthropic", BaseURL: "https://desktop.invalid", Model: "m"},
		{Name: "global-alias", Kind: "openai", BaseURL: "https://global.invalid", Model: "g"},
	}, Credentials: map[string]string{"local-alias": "virtual-local", "global-alias": "virtual-global"}, References: map[string]string{"local/m": "local-alias/m", "global/g": "global-alias/g"}, Preferences: ModelRuntimePreferences{PlannerModel: "global-alias/g", SubagentEffort: "low"}}
	c, err := LoadModelRuntimeSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := bundle.Apply(c, root); err != nil {
		t.Fatal(err)
	}
	local, ok := c.ResolveModel("local-alias/m")
	if !ok || local.Kind != "openai" || local.BaseURL != "https://project.invalid/v1" || local.APIKey() != "project-secret" || local.CredentialProxyURL() != "" {
		t.Fatalf("managed model alias: found=%v kind=%q url=%q credentialMatches=%v proxy=%q", ok, local.Kind, local.BaseURL, local.APIKey() == "project-secret", local.CredentialProxyURL())
	}
	global, ok := c.ResolveModel("global-alias/g")
	if !ok || global.APIKey() != "virtual-global" || global.CredentialProxyURL() != bundle.ProxyURL {
		t.Fatal("managed global model did not retain virtual credential")
	}
	if c.Agent.PlannerModel != "local/m" || c.Agent.VisionModel != "global-alias/g" || c.Agent.SubagentModels["review"] != "global-alias/g" || c.Agent.SubagentEffort != "high" {
		t.Fatalf("project model preferences = %+v", c.RuntimeModelPreferences())
	}
	bundle.Credentials["global-alias"] = "later"
	if global.APIKey() != "virtual-global" {
		t.Fatal("snapshot retained mutable bundle credentials")
	}
}
