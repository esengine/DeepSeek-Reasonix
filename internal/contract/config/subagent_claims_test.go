package config

import (
	"slices"
	"strings"
	"testing"
)

func TestSubagentClaimsAreStrictUnlessTheUserChoseOptimistic(t *testing.T) {
	for value, want := range map[string]bool{"": false, "strict": false, "optimistic": true, "Optimistic": true, " optimistic ": true, "off": false, "nonsense": false} {
		cfg := Default()
		cfg.Agent.SubagentClaims = value
		if got := cfg.SubagentClaimsOptimistic(); got != want {
			t.Errorf("subagent_claims = %q -> optimistic %v, want %v", value, got, want)
		}
	}
}

func TestUserFileChoosesTheSubagentClaims(t *testing.T) {
	cfg, _ := loadScoped(t, "[agent]\nsubagent_claims = \"optimistic\"\n", "")
	if !cfg.SubagentClaimsOptimistic() {
		t.Fatalf("a user file's optimistic was not honoured: %q", cfg.Agent.SubagentClaims)
	}
}

func TestProjectCannotChangeTheSubagentClaims(t *testing.T) {
	cfg, _ := loadScoped(t, "", "[agent]\nsubagent_claims = \"strict\"\n")
	if !cfg.SubagentClaimsOptimistic() {
		t.Fatal("a cloned repo's reasonix.toml tightened the sub-agent claims for the user")
	}
	if !slices.Contains(ignoredKeys(cfg), "agent.subagent_claims") {
		t.Fatalf("ignored = %v, want agent.subagent_claims reported", ignoredKeys(cfg))
	}
}

func TestProjectCannotLoosenAStrictUserChoiceSubagentClaims(t *testing.T) {
	cfg, _ := loadScoped(t, "[agent]\nsubagent_claims = \"strict\"\n", "[agent]\nsubagent_claims = \"optimistic\"\n")
	if cfg.SubagentClaimsOptimistic() {
		t.Fatal("a cloned repo's reasonix.toml loosened the user's own strict subagent_claims")
	}
}

func TestProjectCannotTightenOverAnOptimisticClaimsChoiceEither(t *testing.T) {
	cfg, _ := loadScoped(t, "[agent]\nsubagent_claims = \"optimistic\"\n", "[agent]\nsubagent_claims = \"strict\"\n")
	if !cfg.SubagentClaimsOptimistic() {
		t.Fatal("the project file overrode the user's own subagent_claims")
	}
}

func TestAnOptimisticSubagentClaimsSurvivesASave(t *testing.T) {
	cfg := Default()
	cfg.Agent.SubagentClaims = "optimistic"
	if out := RenderTOMLForScope(cfg, RenderScopeUser); !strings.Contains(out, "subagent_claims = \"optimistic\"") {
		t.Fatalf("rendered config lost the choice:\n%s", out)
	}
	if out := RenderTOMLForScope(Default(), RenderScopeUser); strings.Contains(out, "\nsubagent_claims = ") {
		t.Fatalf("the optimistic default was written as an active key:\n%s", out)
	}
	strict := Default()
	strict.Agent.SubagentClaims = "strict"
	if out := RenderTOMLForScope(strict, RenderScopeUser); !strings.Contains(out, "subagent_claims = \"strict\"") {
		t.Fatalf("a strict choice was dropped on save, so the optimistic default would return:\n%s", out)
	}
}
