package config

import "reasonix/internal/contract/provider"

func cloneProviderPreset(p ProviderPreset) ProviderPreset {
	p.Entries = cloneProviderEntries(p.Entries)
	for i := range p.Entries {
		p.Entries[i].PresetID = p.ID
		p.Entries[i].PresetVersion = ProviderPresetVersion
	}
	return p
}

func cloneProviderEntries(in []ProviderEntry) []ProviderEntry {
	out := make([]ProviderEntry, 0, len(in))
	for _, e := range in {
		out = append(out, cloneProviderEntry(e))
	}
	return out
}

func cloneProviderEntry(e ProviderEntry) ProviderEntry {
	if e.WebSearch != nil {
		value := *e.WebSearch
		e.WebSearch = &value
	}
	if e.ResponsesStateful != nil {
		value := *e.ResponsesStateful
		e.ResponsesStateful = &value
	}
	if e.visionOverride != nil {
		value := *e.visionOverride
		e.visionOverride = &value
	}
	e.Models = append([]string(nil), e.Models...)
	e.VisionModels = append([]string(nil), e.VisionModels...)
	e.SupportedEfforts = append([]string(nil), e.SupportedEfforts...)
	e.Headers, e.ModelAliases = cloneStringMap(e.Headers), cloneStringMap(e.ModelAliases)
	e.ExtraBody = cloneAnyMap(e.ExtraBody)
	e.Price = clonePricing(e.Price)
	e.Prices = clonePricingMap(e.Prices)
	e.ModelOverrides = cloneModelOverrideMap(e.ModelOverrides)
	return e
}

func clonePricingMap(in map[string]*provider.Pricing) map[string]*provider.Pricing {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]*provider.Pricing, len(in))
	for k, v := range in {
		out[k] = clonePricing(v)
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneAnyValue(v)
	}
	return out
}

func cloneAnyValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneAnyMap(x)
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = cloneAnyValue(x[i])
		}
		return out
	default:
		return v
	}
}

func cloneModelOverrideMap(in map[string]ProviderModelOverride) map[string]ProviderModelOverride {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]ProviderModelOverride, len(in))
	for k, v := range in {
		v.SupportedEfforts = append([]string(nil), v.SupportedEfforts...)
		if v.Vision != nil {
			vision := *v.Vision
			v.Vision = &vision
		}
		out[k] = v
	}
	return out
}
