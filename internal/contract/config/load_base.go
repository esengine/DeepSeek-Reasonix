package config

func (r Roots) defaultConfig() *Config {
	mode, err := r.installationCollaboration(false)
	return r.defaultConfigWith(mode, err)
}

func (r Roots) defaultConfigWith(mode string, err error) *Config {
	cfg := Default()
	cfg.roots = r
	applyInstallationCollaboration(cfg, mode, err)
	return cfg
}

func (r Roots) runtimeConfigBase(root string, persist, withProject bool) (*Config, map[string]string) {
	mode, err := r.installationCollaboration(persist)
	var expansionEnv map[string]string
	if withProject {
		expansionEnv = r.loadDotEnvForRoot(root)
	} else {
		r.loadCredentialStoreForScope(root, false)
	}
	cfg := r.defaultConfigWith(mode, err)
	cfg.setExpansionEnv(expansionEnv)
	cfg.CredentialsStore = r.credentialsStoreMode()
	return cfg, expansionEnv
}
