package main

import "reasonix/internal/config"

func resolveDesktopModelSelection(cfg *config.Config, ref string) (*config.ProviderEntry, bool, string) {
	entry, ok := cfg.ResolveModel(ref)
	if selection, selectionErr := config.ParseProviderSelection(cfg, ref); selectionErr == nil {
		if resolved, resolveErr := cfg.ResolveSelection(selection); resolveErr == nil {
			return resolved, true, selection.Ref()
		}
	}
	return entry, ok, ref
}

// appendLegacyAccountFamilyViews keeps the historical family provider name
// visible while account routes remain the canonical persisted entries.
func appendLegacyAccountFamilyViews(views *[]ProviderView, cfg *config.Config, added map[string]bool, root string, resolver *config.CredentialResolver, credentialsRevision string) {
	if views == nil || cfg == nil {
		return
	}
	if len(cfg.Desktop.ProviderAccess) == 0 && configDeclaresProviderAccess(config.UserConfigPath()) {
		return
	}
	for _, family := range []string{"deepseek"} {
		if _, exists := cfg.Provider(family); exists {
			continue
		}
		account, ok := cfg.DefaultAccount(family)
		if !ok {
			continue
		}
		entries, ok := cfg.ResolveAccountProvider(family, account.ID)
		if !ok {
			continue
		}
		for _, entry := range entries {
			if !entry.Configured() || len(entry.ModelList()) == 0 {
				continue
			}
			view := providerViewFromEntryForRootWithResolverAndCredentials(entry, true, added[family], root, resolver, credentialsRevision)
			view.Name = family
			view.Added = true
			view.ProviderID, view.AccountID, view.AccountLabel = account.ProviderID, account.ID, account.Label
			view.AccountEnabled, view.AccountDefault = account.IsEnabled(), account.Default
			*views = append(*views, view)
			break
		}
	}
}
