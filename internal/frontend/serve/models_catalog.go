package serve

import (
	"net/http"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
)

// resolverModels answers /models for a pane whose models resolve through a
// resolver the hub was given. That resolver's catalog is the whole offer: this
// machine's config names models the pane cannot reach, and a picker listing
// them sends each choice to a refusal from the machine doing the resolving.
func (s *Server) resolverModels(w http.ResponseWriter) {
	ctrl := s.ctl()
	current := currentModelRef(ctrl)
	catalog := ctrl.ProviderCatalog()
	out := []modelEntry{}
	for _, d := range catalog {
		if entry, ok := catalogModelEntry(d, current); ok {
			out = append(out, entry)
		}
	}
	writeJSON(w, map[string]any{"current": current, "label": ctrl.Label(), "default": provider.DefaultRef(catalog), "models": out})
}

// isExtensionModelRef reports a plugin/<plugin>/<provider>/<model> ref.
func isExtensionModelRef(ref string) bool {
	parts := strings.Split(strings.TrimSpace(ref), "/")
	return len(parts) >= 4 && parts[0] == "plugin"
}

// catalogModelEntry describes one catalog descriptor for the picker. Its
// capability fields are what the descriptor declares and nothing more.
func catalogModelEntry(d provider.Descriptor, current string) (modelEntry, bool) {
	ref := strings.TrimSpace(d.Ref)
	if ref == "" {
		return modelEntry{}, false
	}
	entry := modelEntry{
		Ref: ref, Active: ref == current, Default: d.Default,
		Vision: d.Vision, Efforts: d.Efforts, Effort: d.DefaultEffort, ContextWindow: d.ContextWindow,
		ForcesThinking: d.ForcesThinking,
	}
	if d.InputPerMillion > 0 || d.OutputPerMillion > 0 {
		entry.Price = &modelPrice{Input: d.InputPerMillion, Output: d.OutputPerMillion, CacheHit: d.CacheHitPerMillion, Currency: d.PricingCurrency}
	}
	if isExtensionModelRef(ref) {
		parts := strings.Split(ref, "/")
		entry.Provider, entry.Model, entry.Kind = strings.Join(parts[:3], "/"), parts[len(parts)-1], "extension"
	} else {
		entry.Provider, entry.Model, _ = strings.Cut(ref, "/")
	}
	if model := strings.TrimSpace(d.Model); model != "" {
		entry.Model = model
	}
	entry.Answers = string(config.AnswersFor(entry.Kind))
	return entry, true
}
