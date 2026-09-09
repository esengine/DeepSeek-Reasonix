package openai

import (
	"testing"

	"reasonix/internal/provider"
)

func TestNewProviderExposesExactModelInputModalities(t *testing.T) {
	modelInfo := &provider.ModelInfo{ID: "vision", InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage}}
	p, err := New(provider.Config{Name: "custom", BaseURL: "https://example.test/v1", Model: "vision", ModelInfo: modelInfo})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, ok := p.(provider.ModelInfoProvider)
	if !ok {
		t.Fatal("OpenAI provider does not implement ModelInfoProvider")
	}
	info := got.ModelInfo()
	if info.ID != "vision" || len(info.InputModalities) != 2 || info.InputModalities[1] != provider.ModalityImage {
		t.Fatalf("ModelInfo = %+v", info)
	}
}
