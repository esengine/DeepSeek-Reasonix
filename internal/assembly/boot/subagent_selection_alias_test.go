package boot

import (
	"testing"

	"reasonix/internal/contract/config"
)

func TestSubagentSelectionPreservesRuntimeForParentAliases(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = []config.ProviderEntry{{Name: "parent", Kind: "openai", Models: []string{"p", "q"}, Default: "p",
		SupportedEfforts: []string{"low", "high"}, DefaultEffort: "high"}}
	parent, ok := cfg.ResolveModel("parent/p")
	if !ok {
		t.Fatal("parent model missing")
	}
	parent.Effort = "low"
	for _, ref := range []string{"", "parent", "parent/p", "p"} {
		t.Run(ref, func(t *testing.T) {
			_, selection, err := subagentProviderSelection(cfg, nil, parent, ref, "")
			if err != nil || selection.Ref != "parent/p" || selection.Effort == nil || *selection.Effort != "low" {
				t.Fatalf("parent runtime selection for %q = %+v, %v", ref, selection, err)
			}
		})
	}
	_, other, err := subagentProviderSelection(cfg, nil, parent, "parent/q", "")
	if err != nil || other.Ref != "parent/q" || other.Effort != nil {
		t.Fatalf("another model inherited the parent's override: %+v, %v", other, err)
	}
	_, explicit, err := subagentProviderSelection(cfg, nil, parent, "parent", "high")
	if err != nil || explicit.Effort == nil || *explicit.Effort != "high" {
		t.Fatalf("explicit effort did not narrow the inherited selection: %+v, %v", explicit, err)
	}
}
