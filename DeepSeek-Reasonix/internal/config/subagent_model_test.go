package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestAgentSubagentModelConfigDecodesFromTOML(t *testing.T) {
	var cfg Config
	if _, err := toml.Decode(`
[agent]
subagent_model = "deepseek-pro"
subagent_models = { explore = "deepseek-pro", "security-review" = "mimo-pro" }
`, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if cfg.Agent.SubagentModel != "deepseek-pro" {
		t.Fatalf("subagent_model = %q, want deepseek-pro", cfg.Agent.SubagentModel)
	}
	if cfg.Agent.SubagentModels["explore"] != "deepseek-pro" {
		t.Fatalf("explore model = %q", cfg.Agent.SubagentModels["explore"])
	}
	if cfg.Agent.SubagentModels["security-review"] != "mimo-pro" {
		t.Fatalf("security-review model = %q", cfg.Agent.SubagentModels["security-review"])
	}
}
