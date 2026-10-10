package config

import "strings"

func (c *Config) lookupModel(ref string) (*ProviderEntry, bool) {
	// "provider/model"
	if prov, model, ok := strings.Cut(ref, "/"); ok {
		if e, found := c.Provider(prov); found && e.HasModel(model) {
			return e.forModel(model), true
		}
	}
	// a provider name → its default model
	if e, found := c.Provider(ref); found {
		return e.forModel(e.DefaultModel()), true
	}
	// a bare model name → the provider that lists it
	for i := range c.Providers {
		if c.Providers[i].HasModel(ref) {
			return c.Providers[i].forModel(ref), true
		}
	}
	return nil, false
}
