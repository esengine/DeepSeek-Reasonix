package config

import "maps"

// WithCredentialContextFrom restores only live provenance and scoped dotenv lookup.
func (e PluginEntry) WithCredentialContextFrom(live PluginEntry) PluginEntry {
	e.Source = live.Source
	e.expansionEnv = maps.Clone(live.expansionEnv)
	return e
}
