package provider

import "testing"

func TestModelInfoSupportsInputRequiresExplicitModality(t *testing.T) {
	if !(ModelInfo{ID: "vision", InputModalities: []ModelModality{ModalityText, ModalityImage}}).SupportsInput(ModalityImage) {
		t.Fatal("image modality should be supported when explicitly declared")
	}
	if (ModelInfo{ID: "text", InputModalities: []ModelModality{ModalityText}}).SupportsInput(ModalityImage) {
		t.Fatal("text-only model must not support image input")
	}
	if (ModelInfo{ID: "unknown"}).SupportsInput(ModalityImage) {
		t.Fatal("unknown modality must not be treated as supported")
	}
}
