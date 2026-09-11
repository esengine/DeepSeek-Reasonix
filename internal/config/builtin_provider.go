package config

// BuiltinProviderEntry returns the built-in provider with name. Remote
// bootstrap materializes it before adding a custom provider because an
// explicit providers table replaces the built-in set.
func BuiltinProviderEntry(name string) (ProviderEntry, bool) {
	for _, provider := range Default().Providers {
		if provider.Name == name {
			return provider, true
		}
	}
	return ProviderEntry{}, false
}
