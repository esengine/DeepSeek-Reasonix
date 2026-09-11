package provider

import "slices"

// ModelModality identifies an input modality accepted by a model. Keep this
// type open for future audio/video/file capabilities; the first implementation
// only routes text and image inputs.
type ModelModality string

const (
	ModalityText  ModelModality = "text"
	ModalityImage ModelModality = "image"
)

// ModelInfo is the adapter-owned, model-level capability metadata. A nil
// InputModalities means that the adapter could not determine the capability;
// an explicit []{"text"} is a deliberate negative declaration.
type ModelInfo struct {
	ID              string          `json:"id"`
	Name            string          `json:"name,omitempty"`
	API             string          `json:"api,omitempty"`
	BaseURL         string          `json:"base_url,omitempty"`
	InputModalities []ModelModality `json:"input_modalities,omitempty"`
	ContextWindow   int             `json:"context_window,omitempty"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
	Reasoning       bool            `json:"reasoning,omitempty"`
	Pricing         *Pricing        `json:"pricing,omitempty"`
}

// ModelInfoProvider is implemented by providers that can expose metadata for
// the exact model instance they were constructed for. It is optional so older
// third-party providers remain source-compatible.
type ModelInfoProvider interface {
	ModelInfo() ModelInfo
}

// SupportsInput reports whether the model explicitly accepts the modality.
func (m ModelInfo) SupportsInput(modality ModelModality) bool {
	return slices.Contains(m.InputModalities, modality)
}
