package config

// credentialEnvNamesForRoot names the credential variables the user's and the
// workspace's configuration declare.
func (r Roots) credentialEnvNamesForRoot(root string) []string {
	return r.credentialEnvNamesForScope(root, true)
}

// credentialEnvNamesForScope is credentialEnvNamesForRoot; withProject false
// leaves out the ones only the workspace names.
func (r Roots) credentialEnvNamesForScope(root string, withProject bool) []string {
	root = resolveRoot(root)
	cfg := Default()

	projectTOML := ProjectConfigPath(root)
	var tomlSources []string
	if uc := r.userConfigLoadPath(); uc != "" {
		_ = mergeFile(cfg, uc)
		tomlSources = append(tomlSources, uc)
	}
	if withProject {
		_ = mergeFile(cfg, projectTOML)
		tomlSources = append(tomlSources, projectTOML)
	}
	if providers, _, _, ok, err := mergeTOMLProviders(tomlSources); err == nil && ok {
		cfg.Providers = providers
	}

	return credentialEnvNamesFromConfig(cfg)
}

func (r Roots) loadCredentialStoreForRoot(root string) {
	r.loadCredentialStoreForScope(root, true)
}

func (r Roots) loadCredentialStoreForScope(root string, withProject bool) {
	names := r.credentialEnvNamesForScope(root, withProject)
	if len(names) == 0 {
		return
	}
	if p := r.UserCredentialsPath(); p != "" {
		loadDotEnvFileAs(p, CredentialSource{Kind: CredentialSourceCredentials, Path: p, Label: "Reasonix credentials (.env)"})
	}
}
