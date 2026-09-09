package provider

import (
	"sort"
	"strings"

	piAI "github.com/sky-valley/pi/ai"
)

// PiCatalogModelInfo adapts the embedded sky-valley/pi catalog to Reasonix's
// provider-neutral model metadata. Only an exact provider/model and matching
// official route are accepted; custom endpoints never inherit catalog facts.
func PiCatalogModelInfo(kind, baseURL, model string) (ModelInfo, bool) {
	route, ok := OfficialOpenCodeGoRoute(kind, baseURL)
	if !ok {
		return ModelInfo{}, false
	}
	for _, candidate := range piAI.GetModels("opencode-go") {
		if candidate == nil || candidate.ID != strings.TrimSpace(model) || !piCatalogRouteMatches(route, candidate) {
			continue
		}
		return modelInfoFromPi(candidate), true
	}
	return ModelInfo{}, false
}

// PiCatalogModelInfoForProvider resolves the installed catalog using the
// configured provider id when it is an exact pi provider id. Endpoint and API
// must also match the catalog entry, preventing a custom gateway from
// accidentally inheriting another vendor's metadata.
func PiCatalogModelInfoForProvider(providerID, kind, baseURL, model string) (ModelInfo, bool) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return ModelInfo{}, false
	}
	api := expectedCatalogAPI(kind)
	configuredURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	for _, candidate := range piAI.GetModels(providerID) {
		if candidate == nil || candidate.ID != strings.TrimSpace(model) || strings.ToLower(strings.TrimSpace(string(candidate.Api))) != api {
			continue
		}
		catalogURL := strings.TrimRight(strings.TrimSpace(candidate.BaseURL), "/")
		if configuredURL != catalogURL {
			continue
		}
		return modelInfoFromPi(candidate), true
	}
	return ModelInfo{}, false
}

func expectedCatalogAPI(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "openai", "chat", "":
		return "openai-completions"
	case "responses":
		return "openai-responses"
	case "anthropic":
		return "anthropic-messages"
	default:
		return ""
	}
}

// PiCatalogModelInfos returns the complete embedded catalog for one pi
// provider ID. Callers should still verify that the configured endpoint and
// protocol match the catalog provider before applying these facts.
func PiCatalogModelInfos(providerID string) []ModelInfo {
	models := piAI.GetModels(strings.TrimSpace(providerID))
	out := make([]ModelInfo, 0, len(models))
	for _, model := range models {
		if model != nil {
			out = append(out, modelInfoFromPi(model))
		}
	}
	return out
}

// PiCatalogOpenCodeGoModelIDs returns the exact model IDs exposed by one
// OpenCode Go wire route in the embedded catalog.
func PiCatalogOpenCodeGoModelIDs(route string) []string {
	models := piCatalogOpenCodeGoModels(route)
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	sort.Strings(ids)
	return ids
}

// PiCatalogOpenCodeGoVisionModelIDs returns the route's catalog models that
// explicitly accept image input.
func PiCatalogOpenCodeGoVisionModelIDs(route string) []string {
	models := piCatalogOpenCodeGoModels(route)
	ids := make([]string, 0, len(models))
	for _, model := range models {
		if modelInfoFromPi(model).SupportsInput(ModalityImage) {
			ids = append(ids, model.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func piCatalogOpenCodeGoModels(route string) []*piAI.Model {
	var wantAPI, wantBaseURL string
	switch route {
	case OpenCodeGoRouteChat:
		wantAPI, wantBaseURL = "openai-completions", "https://opencode.ai/zen/go/v1"
	case OpenCodeGoRouteAnthropic:
		wantAPI, wantBaseURL = "anthropic-messages", "https://opencode.ai/zen/go"
	case OpenCodeGoRouteResponses:
		wantAPI, wantBaseURL = "openai-responses", "https://opencode.ai/zen/go/v1"
	default:
		return nil
	}
	models := make([]*piAI.Model, 0)
	for _, model := range piAI.GetModels("opencode-go") {
		if model != nil && strings.EqualFold(string(model.Api), wantAPI) && strings.TrimRight(model.BaseURL, "/") == wantBaseURL {
			models = append(models, model)
		}
	}
	return models
}

func modelInfoFromPi(model *piAI.Model) ModelInfo {
	modalities := make([]ModelModality, 0, len(model.Input))
	for _, input := range model.Input {
		modality := ModelModality(strings.ToLower(strings.TrimSpace(input)))
		if modality == ModalityText || modality == ModalityImage {
			modalities = append(modalities, modality)
		}
	}
	if len(modalities) == 0 {
		modalities = []ModelModality{ModalityText}
	}
	return ModelInfo{
		ID:              model.ID,
		Name:            model.Name,
		API:             string(model.Api),
		BaseURL:         model.BaseURL,
		InputModalities: modalities,
		ContextWindow:   model.ContextWindow,
		MaxOutputTokens: model.MaxTokens,
		Reasoning:       model.Reasoning,
	}
}

func piCatalogRouteMatches(route string, model *piAI.Model) bool {
	if model == nil {
		return false
	}
	api := strings.ToLower(strings.TrimSpace(string(model.Api)))
	switch route {
	case OpenCodeGoRouteChat:
		return api == "openai-completions"
	case OpenCodeGoRouteAnthropic:
		return api == "anthropic-messages"
	case OpenCodeGoRouteResponses:
		return api == "openai-responses"
	default:
		return false
	}
}
