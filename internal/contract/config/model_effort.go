// model_effort.go — one model's own effort vocabulary on a gateway that serves
// several vendors' models behind one address and key. It is stored as the
// model's model_overrides entry and applied by the same resolution every
// request and every level picker reads, so a level the picker offers is the
// level the request carries.
package config

import (
	"errors"
	"maps"
	"strings"
)

// ErrModelDefaultEffortNotListed refuses a model's default level that is not
// one of the levels declared for that model.
var ErrModelDefaultEffortNotListed = errors.New("default effort is not one of the model's declared levels")

// ModelEffortDeclaration is what a model declares for itself. A model with no
// declaration inherits the connection's supported_efforts, then the model table.
type ModelEffortDeclaration struct {
	Levels  []string
	Default string
}

// ModelEffortDeclaration reports the levels stored for model itself.
func (e *ProviderEntry) ModelEffortDeclaration(model string) (ModelEffortDeclaration, bool) {
	ov, ok := e.modelOverrideForModel(model)
	levels := normalizedEffortLevels(ov.SupportedEfforts)
	if !ok || len(levels) == 0 {
		return ModelEffortDeclaration{}, false
	}
	return ModelEffortDeclaration{Levels: levels, Default: normalizeEffortLevel(ov.DefaultEffort)}, true
}

// ModelReasoningProtocol is the protocol stored for model itself; empty means
// it follows the connection's.
func (e *ProviderEntry) ModelReasoningProtocol(model string) string {
	ov, _ := e.modelOverrideForModel(model)
	return normalizeReasoningProtocol(ov.ReasoningProtocol)
}

// SetModelEffortDeclaration stores levels for model, or clears its declaration
// when levels is empty so the model inherits again. Every other field of the
// model's override is left as it was.
func (e *ProviderEntry) SetModelEffortDeclaration(model string, levels []string, def string) error {
	model = strings.TrimSpace(model)
	if e == nil || model == "" {
		return nil
	}
	levels = normalizedEffortLevels(levels)
	def = normalizeEffortLevel(def)
	if len(levels) == 0 {
		def = ""
	} else if def != "" && !containsString(levels, def) {
		return ErrModelDefaultEffortNotListed
	}
	key, _ := e.modelOverrideKey(model)
	ov := e.ModelOverrides[key]
	ov.SupportedEfforts, ov.DefaultEffort = levels, def
	if modelOverrideEmpty(ov) {
		delete(e.ModelOverrides, key)
		if len(e.ModelOverrides) == 0 {
			e.ModelOverrides = nil
		}
		return nil
	}
	if e.ModelOverrides == nil {
		e.ModelOverrides = map[string]ProviderModelOverride{}
	}
	e.ModelOverrides[key] = ov
	return nil
}

// InheritedEffortCapability is the ladder model gets when it declares nothing
// of its own: what it would resolve to with its declaration removed.
func (e *ProviderEntry) InheritedEffortCapability(model string) EffortCapability {
	if e == nil {
		return EffortCapability{}
	}
	cp := *e
	if key, ok := cp.modelOverrideKey(model); ok {
		cp.ModelOverrides = maps.Clone(cp.ModelOverrides)
		ov := cp.ModelOverrides[key]
		ov.SupportedEfforts, ov.DefaultEffort = nil, ""
		cp.ModelOverrides[key] = ov
	}
	return EffortCapabilityForEntry(cp.forModel(model))
}

// forModel is the entry resolved onto one of its models: the model's price,
// its overrides, then what is known about the model underneath them.
func (e *ProviderEntry) forModel(model string) *ProviderEntry {
	cp := *e
	cp.Model = model
	cp.applyModelPrice()
	cp.applyModelOverride()
	cp.applyModelCapabilities()
	return &cp
}

// modelOverrideKey is the stored key for model, matched as model resolution
// matches it; a model with no entry yet keys under its own trimmed id.
func (e *ProviderEntry) modelOverrideKey(model string) (string, bool) {
	model = strings.TrimSpace(model)
	if e == nil || model == "" {
		return model, false
	}
	if _, ok := e.ModelOverrides[model]; ok {
		return model, true
	}
	for k := range e.ModelOverrides {
		if strings.EqualFold(strings.TrimSpace(k), model) {
			return k, true
		}
	}
	return model, false
}
