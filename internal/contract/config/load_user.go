package config

// LoadUserConfigReadOnly loads only the user-global config of the process
// binding. See Roots.LoadUserConfigReadOnly.
func LoadUserConfigReadOnly() (*Config, error) { return processRoots().LoadUserConfigReadOnly() }

// LoadUserConfigReadOnly loads only the user-global config: no project
// reasonix.toml, .mcp.json, project .env expansion or installed packages, and
// no on-disk migration. A host feature that must not run anything a checkout
// configures reads its settings from here; a broken file falls back to the
// last-known-good snapshot, then to defaults, exactly like the merged load.
func (r Roots) LoadUserConfigReadOnly() (*Config, error) {
	cfg := r.defaultConfig()
	if path := r.userConfigLoadPath(); path != "" {
		if _, err := resolveConfigAccessPath(path, true); err != nil {
			return nil, err
		}
		if _, err := mergeFileSnapshot(cfg, path); err != nil {
			cfg = r.defaultConfig()
			if lkgErr := r.loadLastKnownGoodUserConfig(cfg); lkgErr != nil {
				cfg = r.defaultConfig()
			}
		}
	}
	normalizeConfigForEdit(cfg)
	return cfg, nil
}
