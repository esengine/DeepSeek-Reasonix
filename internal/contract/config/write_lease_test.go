package config

import (
	"slices"
	"strings"
	"testing"
)

func TestWriteLeaseIsStrictUnlessTheUserChoseOptimistic(t *testing.T) {
	for value, want := range map[string]bool{"": false, "strict": false, "optimistic": true, "Optimistic": true, " optimistic ": true, "off": false, "nonsense": false} {
		cfg := Default()
		cfg.Agent.WriteLease = value
		if got := cfg.WriteLeaseOptimistic(); got != want {
			t.Errorf("write_lease = %q -> optimistic %v, want %v", value, got, want)
		}
	}
}

func TestUserFileChoosesTheWriteLease(t *testing.T) {
	cfg, _ := loadScoped(t, "[agent]\nwrite_lease = \"optimistic\"\n", "")
	if !cfg.WriteLeaseOptimistic() {
		t.Fatalf("a user file's optimistic was not honoured: %q", cfg.Agent.WriteLease)
	}
}

func TestProjectCannotChangeTheWriteLease(t *testing.T) {
	cfg, _ := loadScoped(t, "", "[agent]\nwrite_lease = \"strict\"\n")
	if !cfg.WriteLeaseOptimistic() {
		t.Fatal("a cloned repo's reasonix.toml tightened the cross-session write lease for the user")
	}
	if !slices.Contains(ignoredKeys(cfg), "agent.write_lease") {
		t.Fatalf("ignored = %v, want agent.write_lease reported", ignoredKeys(cfg))
	}
}

func TestProjectCannotLoosenAStrictUserChoiceWriteLease(t *testing.T) {
	cfg, _ := loadScoped(t, "[agent]\nwrite_lease = \"strict\"\n", "[agent]\nwrite_lease = \"optimistic\"\n")
	if cfg.WriteLeaseOptimistic() {
		t.Fatal("a cloned repo's reasonix.toml loosened the user's own strict write_lease")
	}
}

func TestProjectCannotTightenOverAnOptimisticUserChoiceEither(t *testing.T) {
	cfg, _ := loadScoped(t, "[agent]\nwrite_lease = \"optimistic\"\n", "[agent]\nwrite_lease = \"strict\"\n")
	if !cfg.WriteLeaseOptimistic() {
		t.Fatal("the project file overrode the user's own write_lease")
	}
}

func TestAnOptimisticWriteLeaseSurvivesASave(t *testing.T) {
	cfg := Default()
	cfg.Agent.WriteLease = "optimistic"
	if out := RenderTOMLForScope(cfg, RenderScopeUser); !strings.Contains(out, "write_lease = \"optimistic\"") {
		t.Fatalf("rendered config lost the choice:\n%s", out)
	}
	if out := RenderTOMLForScope(Default(), RenderScopeUser); strings.Contains(out, "\nwrite_lease = ") {
		t.Fatalf("the optimistic default was written as an active key:\n%s", out)
	}
	strict := Default()
	strict.Agent.WriteLease = "strict"
	if out := RenderTOMLForScope(strict, RenderScopeUser); !strings.Contains(out, "write_lease = \"strict\"") {
		t.Fatalf("a strict choice was dropped on save, so the optimistic default would return:\n%s", out)
	}
}

func TestSetWriteLeaseRoundTripsThroughTheSameReader(t *testing.T) {
	cfg := Default()
	cfg.SetWriteLease(true)
	if !cfg.WriteLeaseOptimistic() || cfg.Agent.WriteLease != "optimistic" {
		t.Fatalf("after SetWriteLease(true): %q", cfg.Agent.WriteLease)
	}
	cfg.SetWriteLease(false)
	if cfg.WriteLeaseOptimistic() || cfg.Agent.WriteLease != "strict" {
		t.Fatalf("after SetWriteLease(false): %q", cfg.Agent.WriteLease)
	}
}
