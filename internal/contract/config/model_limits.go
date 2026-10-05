// model_limits.go — one model's own context window and output cap on a
// connection whose models do not share them. They are stored as the model's
// model_overrides entry, which every request and the compaction trigger read.
package config

import (
	"errors"
	"maps"
	"strings"
)

// ErrModelContextWindowNegative refuses a per-model window below zero; zero is
// how a model inherits the connection's.
var ErrModelContextWindowNegative = errors.New("a model's context window cannot be negative")

// ModelLimits is what a model declares for itself. Zero inherits; a negative
// output cap is the stored spelling of "send no cap" and is kept as written.
type ModelLimits struct {
	ContextWindow   int
	MaxOutputTokens int
}

// ModelLimits reports the limits stored for model itself.
func (e *ProviderEntry) ModelLimits(model string) (ModelLimits, bool) {
	ov, ok := e.modelOverrideForModel(model)
	if !ok || (ov.ContextWindow <= 0 && ov.MaxOutputTokens == 0) {
		return ModelLimits{}, false
	}
	return ModelLimits{ContextWindow: max(ov.ContextWindow, 0), MaxOutputTokens: ov.MaxOutputTokens}, true
}

// SetModelLimits stores limits for model, or clears them when both are zero so
// the model inherits again. Every other field of the model's override is kept.
func (e *ProviderEntry) SetModelLimits(model string, l ModelLimits) error {
	model = strings.TrimSpace(model)
	if e == nil || model == "" {
		return nil
	}
	if l.ContextWindow < 0 {
		return ErrModelContextWindowNegative
	}
	key, _ := e.modelOverrideKey(model)
	ov := e.ModelOverrides[key]
	ov.ContextWindow, ov.MaxOutputTokens = l.ContextWindow, l.MaxOutputTokens
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

// InheritedLimits is what model runs on when it declares no limits: the entry
// resolved with only those two fields of its override removed.
func (e *ProviderEntry) InheritedLimits(model string) ModelLimits {
	if e == nil {
		return ModelLimits{}
	}
	cp := *e
	if key, ok := cp.modelOverrideKey(model); ok {
		cp.ModelOverrides = maps.Clone(cp.ModelOverrides)
		ov := cp.ModelOverrides[key]
		ov.ContextWindow, ov.MaxOutputTokens = 0, 0
		cp.ModelOverrides[key] = ov
	}
	resolved := cp.forModel(model)
	return ModelLimits{ContextWindow: resolved.ContextWindow, MaxOutputTokens: resolved.MaxOutputTokens}
}
