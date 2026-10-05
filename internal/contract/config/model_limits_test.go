package config

import (
	"errors"
	"testing"
)

func TestSetModelLimitsKeepsTheRestOfTheOverrideAndClears(t *testing.T) {
	e := &ProviderEntry{ContextWindow: 100_000, Models: []string{"a"}, ModelOverrides: map[string]ProviderModelOverride{
		"a": {DefaultEffort: "high", SupportedEfforts: []string{"high"}},
	}}
	if err := e.SetModelLimits("a", ModelLimits{ContextWindow: 8000, MaxOutputTokens: -1}); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.ModelLimits("a"); got.ContextWindow != 8000 || got.MaxOutputTokens != -1 {
		t.Fatalf("limits = %+v", got)
	}
	if e.InheritedLimits("a").ContextWindow != 100_000 {
		t.Fatalf("inherited = %+v", e.InheritedLimits("a"))
	}
	if err := e.SetModelLimits("a", ModelLimits{}); err != nil {
		t.Fatal(err)
	}
	if len(e.ModelOverrides["a"].SupportedEfforts) != 1 {
		t.Fatal("clearing limits dropped the effort declaration")
	}
	e.ModelOverrides["a"] = ProviderModelOverride{ContextWindow: 5}
	_ = e.SetModelLimits("a", ModelLimits{})
	if e.ModelOverrides != nil {
		t.Fatalf("an emptied override stayed: %+v", e.ModelOverrides)
	}
	if err := e.SetModelLimits("a", ModelLimits{ContextWindow: -1}); !errors.Is(err, ErrModelContextWindowNegative) {
		t.Fatalf("err = %v", err)
	}
}
