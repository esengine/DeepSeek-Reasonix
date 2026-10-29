package config

// credentialEnvNamesForRoot names the credential variables the user's and the
// workspace's configuration declare.
func (r Roots) credentialEnvNamesForRoot(root string) []string {
	readPaths := configReadPaths{}
	defer readPaths.close()
	return r.credentialEnvNamesForScope(root, true, readPaths)
}

// credentialEnvNamesForScope is credentialEnvNamesForRoot; withProject false
// leaves out the ones only the workspace names.
func (r Roots) credentialEnvNamesForScope(root string, withProject bool, readPaths configReadPaths) []string {
	root = resolveRoot(root)
	cfg := Default()

	projectTOML := ProjectConfigPath(root)
	var tomlSources []string
	if uc := r.userConfigLoadPath(); uc != "" {
		_, _ = readPaths.mergeFileSnapshot(cfg, uc)
		tomlSources = append(tomlSources, uc)
	}
	if withProject {
		_, _ = readPaths.mergeFileSnapshot(cfg, projectTOML)
		tomlSources = append(tomlSources, projectTOML)
	}
	if providers, _, _, ok, err := readPaths.mergeTOMLProviders(tomlSources); err == nil && ok {
		cfg.Providers = providers
	}

	return credentialEnvNamesFromConfig(cfg)
}

func (r Roots) loadCredentialStoreForScope(root string, withProject bool, readPaths configReadPaths) {
	names := r.credentialEnvNamesForScope(root, withProject, readPaths)
	if len(names) == 0 {
		return
	}
	if p := r.UserCredentialsPath(); p != "" {
		loadDotEnvFileAs(p, CredentialSource{Kind: CredentialSourceCredentials, Path: p, Label: "Reasonix credentials (.env)"})
	}
}

func (r Roots) credentialsStoreMode() string {
	return r.credentialsStoreModeWithPaths(nil)
}

func (r Roots) readCredentialsStoreMode(readPaths configReadPaths) string {
	var partial struct {
		CredentialsStore string `toml:"credentials_store"`
	}
	if path := r.userConfigLoadPath(); path != "" {
		_, _ = readPaths.decodeTOMLFile(path, &partial)
	}
	return normalizeCredentialsStore(partial.CredentialsStore)
}
