package config

import "github.com/BurntSushi/toml"

func markProjectEditProvenance(c *Config, path string, meta toml.MetaData) {
	markExplicitDefaultProjectSkillKeys(c, path, meta)
	if isUserConfigPath(path) || !meta.IsDefined("providers") {
		return
	}
	c.explicitProjectProviderNames = make(map[string]bool, len(c.Providers))
	for _, p := range c.Providers {
		c.explicitProjectProviderNames[providerMergeKey(p)] = true
	}
}

func normalizeConfigForEditWithProjectBaseline(c *Config, path string) bool {
	changed := normalizeConfigForEdit(c)
	if !isUserConfigPath(path) {
		c.projectProviderEditBaseline = cloneProviderEntries(c.Providers)
	}
	return changed
}
