// provider_model_effort.go — effort levels declared per model on a connection
// that serves several vendors' models under one address and key.
package serve

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/contract/config"
)

// modelEffortView is one model's effort vocabulary and its default.
type modelEffortView struct {
	SupportedEfforts []string `json:"supportedEfforts"`
	DefaultEffort    string   `json:"defaultEffort,omitempty"`
	// Official marks an inherited ladder that is the vendor's documented contract.
	Official bool `json:"official,omitempty"`
}

// modelEffortsOf is what each chat model declares for itself, as stored.
func modelEffortsOf(p *config.ProviderEntry) map[string]modelEffortView {
	out := map[string]modelEffortView{}
	for _, model := range p.ChatModelList() {
		if d, ok := p.ModelEffortDeclaration(model); ok {
			out[model] = modelEffortView{SupportedEfforts: d.Levels, DefaultEffort: d.Default}
		}
	}
	return orNilMap(out)
}

// inheritedEffortsOf is the ladder each chat model runs on when it declares
// nothing, so the form can say what "inherit" means for that model.
func inheritedEffortsOf(p *config.ProviderEntry) map[string]modelEffortView {
	out := map[string]modelEffortView{}
	for _, model := range p.ChatModelList() {
		capability := p.InheritedEffortCapability(model)
		if !capability.Supported {
			continue
		}
		levels := slices.DeleteFunc(slices.Clone(capability.Levels), func(l string) bool { return l == "auto" })
		def := capability.Default
		if def == "auto" {
			def = ""
		}
		out[model] = modelEffortView{SupportedEfforts: levels, DefaultEffort: def, Official: p.InheritedEffortOfficial(model)}
	}
	return orNilMap(out)
}

// modelProtocolsOf is the reasoning protocol a chat model declares for itself;
// a fixed-vocabulary protocol leaves a model's declared levels dormant.
func modelProtocolsOf(p *config.ProviderEntry) map[string]string {
	out := map[string]string{}
	for _, model := range p.ChatModelList() {
		if protocol := p.ModelReasoningProtocol(model); protocol != "" {
			out[model] = protocol
		}
	}
	return orNilMap(out)
}

func orNilMap[V any](m map[string]V) map[string]V {
	if len(m) == 0 {
		return nil
	}
	return m
}

// applyModelEfforts makes the form's per-model answer the whole answer for the
// models it lists: a model sent with levels declares them, one sent without or
// left out inherits again. Models the form does not list keep what they have.
func applyModelEfforts(entry *config.ProviderEntry, models []string, sent *map[string]modelEffortView) *editRefusal {
	if sent == nil {
		return nil
	}
	for model := range *sent {
		if !slices.Contains(models, strings.TrimSpace(model)) {
			return &editRefusal{"provider.model_effort_unlisted",
				fmt.Sprintf("%q is not one of the selected models", model), map[string]any{"model": model}}
		}
	}
	for _, model := range models {
		want := lookupModelEffort(*sent, model)
		err := entry.SetModelEffortDeclaration(model, want.SupportedEfforts, want.DefaultEffort)
		if errors.Is(err, config.ErrModelDefaultEffortNotListed) {
			level := strings.ToLower(strings.TrimSpace(want.DefaultEffort))
			return &editRefusal{"provider.model_default_effort_not_listed",
				fmt.Sprintf("default effort %q is not one of the levels declared for %s", level, model),
				map[string]any{"model": model, "level": level}}
		}
	}
	return nil
}

// editRefusal is a 400 the provider edit answers with.
type editRefusal struct {
	code, message string
	detail        map[string]any
}

func lookupModelEffort(sent map[string]modelEffortView, model string) modelEffortView {
	for k, v := range sent {
		if strings.TrimSpace(k) == model {
			return v
		}
	}
	return modelEffortView{}
}
