package config

import "testing"

func TestDefaultAutoPlanOff(t *testing.T) {
	if got := Default().Agent.AutoPlan; got != "off" {
		t.Fatalf("default auto_plan = %q, want off", got)
	}
}

func TestDefaultReasoningLanguageAuto(t *testing.T) {
	if got := Default().ReasoningLanguage(); got != "auto" {
		t.Fatalf("default reasoning_language = %q, want auto", got)
	}
}

func TestDefaultMemoryRecallPolicy(t *testing.T) {
	policy := Default().MemoryRecallPolicy()
	if policy.Diversity == nil || !*policy.Diversity || policy.Staleness == nil || !*policy.Staleness {
		t.Fatalf("default memory recall policy should enable diversity and staleness: %+v", policy)
	}
	if policy.DiversityWeight != DefaultMemoryRecallDiversityWeight ||
		policy.DuplicateThreshold != DefaultMemoryRecallDuplicateThreshold ||
		policy.StalenessHalfLifeDays != DefaultMemoryRecallStalenessHalfLifeDays {
		t.Fatalf("unexpected default memory recall policy: %+v", policy)
	}
	disabled := false
	cfg := Default()
	cfg.Agent.MemoryRecall.Diversity = &disabled
	cfg.Agent.MemoryRecall.Staleness = &disabled
	policy = cfg.MemoryRecallPolicy()
	if *policy.Diversity || *policy.Staleness {
		t.Fatalf("explicit memory recall opt-out was not preserved: %+v", policy)
	}
}
func TestDefaultDesktopAppearanceAutoGraphite(t *testing.T) {
	cfg := Default()
	if got := cfg.DesktopTheme(); got != "auto" {
		t.Fatalf("default desktop theme = %q, want auto", got)
	}
	if got := cfg.DesktopThemeStyle(); got != "" {
		t.Fatalf("default desktop theme style = %q, want empty so frontend resolves graphite", got)
	}
}

func TestDefaultDesktopMetricsOn(t *testing.T) {
	cfg := Default()
	if !cfg.DesktopMetrics() {
		t.Fatal("default desktop metrics = false, want true")
	}
	disabled := false
	cfg.Desktop.Metrics = &disabled
	if cfg.DesktopMetrics() {
		t.Fatal("desktop metrics explicit false = true, want false")
	}
}
