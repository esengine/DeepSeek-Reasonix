package main

import (
	"reflect"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/provider/openai"
)

func TestProviderViewKeepsOfficialDeepSeekVisionSelection(t *testing.T) {
	p := config.ProviderEntry{
		Name:         "deepseek",
		Kind:         "openai",
		BaseURL:      "https://api.deepseek.com",
		Models:       []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		VisionModels: []string{"deepseek-v4-flash", "deepseek-v4-pro"},
	}
	view := providerViewFromEntry(p, true, true)
	if view.VisionCapability != "configurable" {
		t.Fatalf("VisionCapability = %q, want configurable", view.VisionCapability)
	}
	if !reflect.DeepEqual(view.VisionModels, []string{"deepseek-v4-flash", "deepseek-v4-pro"}) {
		t.Fatalf("ProviderView must round-trip Settings image-input checkboxes: %v", view.VisionModels)
	}
	resolved := p
	resolved.Model = "deepseek-v4-pro"
	if config.EffectiveVision(&resolved) {
		t.Fatal("Flash/Pro image-input checkboxes must not enable runtime image payloads")
	}

	p.Models = append(p.Models, openai.OfficialDeepSeekVisionModel)
	p.VisionModels = []string{openai.OfficialDeepSeekVisionModel}
	view = providerViewFromEntry(p, true, true)
	if !reflect.DeepEqual(view.VisionModels, []string{openai.OfficialDeepSeekVisionModel}) {
		t.Fatalf("ProviderView.VisionModels = %v, want selected vision SKU", view.VisionModels)
	}
	resolved = p
	resolved.Model = openai.OfficialDeepSeekVisionModel
	if !config.EffectiveVision(&resolved) {
		t.Fatal("selecting the official DeepSeek vision SKU with image input checked must enable image input")
	}
}

func TestSaveProviderPersistsOfficialDeepSeekVisionModels(t *testing.T) {
	isolateDesktopUserDirs(t)

	if err := NewApp().SaveProvider(ProviderView{
		Name:            "deepseek",
		Kind:            "openai",
		BaseURL:         "https://api.deepseek.com",
		Models:          []string{"deepseek-v4-flash", "deepseek-v4-pro", openai.OfficialDeepSeekVisionModel},
		VisionModels:    []string{openai.OfficialDeepSeekVisionModel},
		VisionModelsSet: true,
		Default:         "deepseek-v4-flash",
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := cfg.Provider("deepseek")
	if !ok {
		t.Fatal("saved provider not found")
	}
	if !reflect.DeepEqual(got.VisionModels, []string{openai.OfficialDeepSeekVisionModel}) {
		t.Fatalf("saved official DeepSeek vision_models = %v, want selected vision SKU", got.VisionModels)
	}
	flash := *got
	flash.Model = "deepseek-v4-flash"
	if config.EffectiveVision(&flash) {
		t.Fatal("saved Flash must stay text-only on the official DeepSeek endpoint")
	}
	sku := *got
	sku.Model = openai.OfficialDeepSeekVisionModel
	if !config.EffectiveVision(&sku) {
		t.Fatal("saved vision SKU with image input checked must enable image input")
	}
}
