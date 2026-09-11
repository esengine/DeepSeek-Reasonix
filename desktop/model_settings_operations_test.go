package main

import (
	"context"
	"os"
	"testing"

	"reasonix/internal/config"
)

func TestModelSettingsEveryOperationWithoutSessionRejectsForeignFields(t *testing.T) {
	kinds := []string{"default", "planner", "vision", "search", "subagent", "subagent_effort", "profile_model", "profile_effort", "depth", "concurrency", "writers", "provider_save", "credential", "web_search_capability", "connection_add", "official_add", "preset_add", "preset_reset", "protocol_upgrade", "catalogs", "provider_remove", "access_remove", "rename"}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			isolateDesktopUserDirs(t)
			_, ref := configureSwitchableDefaultModels(t)
			app := NewApp()
			app.ctx = context.Background()
			change := sessionlessModelSettingsOperation(t, app, kind, ref)
			beforeConfig, _ := os.ReadFile(config.UserConfigPath())
			beforeKeys, _ := os.ReadFile(config.UserCredentialsPath())
			invalid := change
			invalid.RequestID = "foreign-fields"
			invalid.ExpectedFingerprint = app.Settings().ModelSettingsFingerprint
			if change.Kind == "preference" {
				invalid.BaseURL = "https://example.invalid/foreign"
			} else {
				invalid.Field = "foreign"
			}
			if result := app.ApplyModelSettings(invalid); result.Persisted || len(result.Issues) == 0 {
				t.Fatalf("foreign fields accepted: %+v", result)
			}
			afterConfig, _ := os.ReadFile(config.UserConfigPath())
			afterKeys, _ := os.ReadFile(config.UserCredentialsPath())
			if string(beforeConfig) != string(afterConfig) || string(beforeKeys) != string(afterKeys) {
				t.Fatal("validation wrote configuration or credentials")
			}
			change.RequestID = "valid-edit"
			change.ExpectedFingerprint = app.Settings().ModelSettingsFingerprint
			result := app.ApplyModelSettings(change)
			if !result.Persisted || result.Application != "not_required" || len(result.Targets) != 0 {
				t.Fatalf("sessionless save: %+v", result)
			}
			if len(app.tabs) != 0 || len(app.detachedSessions) != 0 || app.activeTabID != "" {
				t.Fatal("model settings created an implicit session")
			}
			if kind == "catalogs" && len(result.AppliedCatalogs) != 1 {
				t.Fatal("catalog update was not committed")
			}
		})
	}
}

func sessionlessModelSettingsOperation(t *testing.T, app *App, kind, ref string) ModelSettingsChange {
	t.Helper()
	key, enabled := "new-operation-key", false
	change := ModelSettingsChange{Kind: kind}
	switch kind {
	case "default", "planner", "subagent", "profile_model":
		change.Kind, change.Field, change.Ref = "preference", kind, ref
		if kind == "profile_model" {
			change.Name = "reviewer"
		}
	case "vision", "search", "subagent_effort", "profile_effort":
		change.Kind, change.Field, change.Ref = "preference", kind, "auto"
		if kind == "profile_effort" {
			change.Name = "reviewer"
		}
	case "depth", "concurrency", "writers":
		change.Kind, change.Field, change.Number = "preference", kind, 2
	case "provider_save":
		change.Provider = &ProviderView{Name: "extra", Kind: "openai", BaseURL: "https://example.invalid/v1", Models: []string{"m"}}
		change.Key = &key
	case "credential":
		change.Name, change.Key = "old", &key
	case "web_search_capability":
		if _, err := app.AddOfficialProviderAccess("deepseek", key); err != nil {
			t.Fatal(err)
		}
		change.Names, change.Enabled = []string{"deepseek"}, &enabled
	case "connection_add":
		change.Name, change.Key = "old", &key
	case "official_add":
		change.Name, change.Key = "deepseek", &key
	case "preset_add", "preset_reset":
		change.PresetID = config.CuratedProviderPresets()[0].ID
		if kind == "preset_add" {
			change.Key = &key
		} else if _, err := app.AddProviderPresetAccess(change.PresetID, key); err != nil {
			t.Fatal(err)
		}
	case "protocol_upgrade":
		if _, err := app.AddOfficialProviderAccess("deepseek", key); err != nil {
			t.Fatal(err)
		}
		change.Name = "deepseek-flash"
	case "catalogs":
		cfg := config.LoadForEdit(config.UserConfigPath())
		entry, _ := cfg.Provider("old")
		change.Catalogs = []ProviderModelCatalogUpdate{{Name: "old", ExpectedFingerprint: providerModelCatalogFingerprint(*entry), Models: []string{"old-model", "added-model"}}}
	case "provider_remove", "access_remove", "rename":
		change.Names = []string{"old"}
		if kind == "rename" {
			change.Ref = "Renamed connection"
		}
	default:
		t.Fatalf("missing operation fixture: %s", kind)
	}
	return change
}
