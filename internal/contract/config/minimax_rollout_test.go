package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestMiniMaxFlashCatalogAndEffort(t *testing.T) {
	for _, preset := range CuratedProviderPresets() {
		for _, entry := range preset.Entries {
			for _, model := range entry.ModelList() {
				if strings.Contains(strings.ToLower(model), "minimax-m2") {
					t.Errorf("fresh preset %s still offers retired model %s", preset.ID, model)
				}
			}
		}
	}
	for _, id := range []string{"minimax-cn-api", "minimax-global-api", "minimax-cn-anthropic", "minimax-global-anthropic"} {
		preset, _ := CuratedProviderPreset(id)
		entry := preset.Entries[0]
		if !reflect.DeepEqual(entry.ModelList(), []string{"MiniMax-M3.1-Flash", "MiniMax-M3"}) || entry.DefaultModel() != "MiniMax-M3.1-Flash" {
			t.Fatalf("%s: models/default = %v/%s", id, entry.ModelList(), entry.DefaultModel())
		}
		cfg := Config{Providers: preset.Entries}
		flash, ok := cfg.ResolveModel(id + "/MiniMax-M3.1-Flash")
		if !ok || flash.ContextWindow != 1000000 || !EffectiveVision(flash) {
			t.Fatalf("%s: resolved Flash = %+v", id, flash)
		}
		cap := EffortCapabilityForEntry(flash)
		if !reflect.DeepEqual(cap.Levels, []string{"auto", "low", "medium", "high", "xhigh", "max"}) || cap.Default != "max" {
			t.Fatalf("%s: Flash effort capability = %+v", id, cap)
		}
		for _, level := range cap.Levels[1:] {
			if got, err := NormalizeEffort(flash, level); err != nil || got != level {
				t.Errorf("%s/%s: %s, %v", id, level, got, err)
			}
		}
		for _, level := range []string{"none", "disabled", "off", "adaptive"} {
			if _, err := NormalizeEffort(flash, level); err == nil {
				t.Errorf("%s accepted invalid depth %s", id, level)
			}
		}
		if EffortFieldForEntry(flash) == "" {
			t.Errorf("%s: missing effort field", id)
		}
	}
}

func TestMiniMaxSavedChoicesSurvivePresetBackfill(t *testing.T) {
	preset, _ := CuratedProviderPreset("minimax-cn-api")
	entry := preset.Entries[0]
	entry.Models = []string{"MiniMax-M2.7", "user-model"}
	entry.Default = "MiniMax-M2.7"
	entry.ContextWindow = 123456
	entry.ExtraBody = map[string]any{"user_flag": true}
	cfg := Config{Providers: []ProviderEntry{entry}}
	upgradeShippedPresets(&cfg)
	for _, model := range []string{"MiniMax-M2.7", "user-model"} {
		resolved, ok := cfg.ResolveModel(entry.Name + "/" + model)
		if !ok || resolved.Model != model || resolved.ContextWindow != 123456 || resolved.ExtraBody["user_flag"] != true {
			t.Fatalf("saved model changed: %+v", resolved)
		}
	}
	if cfg.Providers[0].DefaultModel() != "MiniMax-M2.7" {
		t.Fatal("saved default was silently replaced")
	}
}
