package config

func cloneProviderPreset(p ProviderPreset) ProviderPreset {
	p.Entries = cloneProviderEntries(p.Entries)
	p.AccountGroupID = p.resolvedAccountGroupID()
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
	e.Headers = cloneStringMap(e.Headers)
	e.ExtraBody = cloneAnyMap(e.ExtraBody)
	e.Price = clonePricing(e.Price)
	e.Prices = clonePricingMap(e.Prices)
	e.ModelOverrides = cloneModelOverrideMap(e.ModelOverrides)
	return e
}
