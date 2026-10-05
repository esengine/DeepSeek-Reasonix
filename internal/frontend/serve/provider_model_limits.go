// provider_model_limits.go — context window and output cap declared per model
// on a connection whose models differ in both.
package serve

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/contract/config"
)

// modelLimitsView is one model's own limits, or the ones it inherits. Zero is
// "not set": a model that inherits nothing stays out of the map.
type modelLimitsView struct {
	ContextWindow   int `json:"contextWindow,omitempty"`
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

func modelLimitsOf(p *config.ProviderEntry) map[string]modelLimitsView {
	out := map[string]modelLimitsView{}
	for _, model := range p.ChatModelList() {
		if l, ok := p.ModelLimits(model); ok {
			out[model] = modelLimitsView{ContextWindow: l.ContextWindow, MaxOutputTokens: l.MaxOutputTokens}
		}
	}
	return orNilMap(out)
}

// inheritedLimitsOf is what each chat model runs on with no limits of its own,
// so the form can show the value a blank field means.
func inheritedLimitsOf(p *config.ProviderEntry) map[string]modelLimitsView {
	out := map[string]modelLimitsView{}
	for _, model := range p.ChatModelList() {
		l := p.InheritedLimits(model)
		if l.ContextWindow != 0 || l.MaxOutputTokens != 0 {
			out[model] = modelLimitsView{ContextWindow: l.ContextWindow, MaxOutputTokens: l.MaxOutputTokens}
		}
	}
	return orNilMap(out)
}

// applyModelLimits makes the form's answer the whole answer for the models it
// lists: one sent with a value declares it, one sent empty or left out inherits
// again. Models the form does not list keep what they have.
func applyModelLimits(entry *config.ProviderEntry, models []string, sent *map[string]modelLimitsView) *editRefusal {
	if sent == nil {
		return nil
	}
	for model := range *sent {
		if !slices.Contains(models, strings.TrimSpace(model)) {
			return &editRefusal{"provider.model_limits_unlisted",
				fmt.Sprintf("%q is not one of the selected models", model), map[string]any{"model": model}}
		}
	}
	for _, model := range models {
		var want modelLimitsView
		for k, v := range *sent {
			if strings.TrimSpace(k) == model {
				want = v
			}
		}
		err := entry.SetModelLimits(model, config.ModelLimits{ContextWindow: want.ContextWindow, MaxOutputTokens: want.MaxOutputTokens})
		if errors.Is(err, config.ErrModelContextWindowNegative) {
			return &editRefusal{"provider.bad_model_context_window",
				fmt.Sprintf("context window for %s cannot be negative", model), map[string]any{"model": model}}
		}
	}
	return nil
}
