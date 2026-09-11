package main

import (
	"reasonix/internal/config"
	"testing"
)

func TestProviderCatalogViewsAndInstallPreserveExistingConnections(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := NewApp()
	if _, err := a.AddProviderPresetAccess("glm-coding-plan-cn", ""); err != nil {
		t.Fatal(err)
	}
	before := config.LoadForEdit(config.UserConfigPath())
	original, ok := before.Provider("glm-coding-plan-cn")
	if !ok {
		t.Fatal("missing original")
	}
	if _, err := a.AddProviderPresetAccess("openrouter", ""); err != nil {
		t.Fatal(err)
	}
	after := config.LoadForEdit(config.UserConfigPath())
	current, ok := after.Provider(original.Name)
	if !ok || !config.ProviderEntriesConfigEqual(*original, *current) {
		t.Fatal("adding another brand changed an existing connection")
	}
	views := a.Settings().ProviderPresets
	found := false
	for _, view := range views {
		if view.ID == "glm-coding-plan-cn" {
			found = true
			if view.Catalog.BrandID != "zai" || view.Catalog.Product != "coding" || view.Catalog.Format != "openai" || view.Status != "installed" {
				t.Fatalf("incorrect catalog view: %+v", view.Catalog)
			}
		}
	}
	if !found {
		t.Fatal("missing installed route")
	}
}
