package main

import (
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/config"
)

// AddOfficialProviderAccess adds one curated desktop provider template to the
// Settings > Model > Access list. The runtime default providers still exist
// independently; this only records the user's explicit access setup.
func (a *App) AddOfficialProviderAccess(kind, key string) (string, error) {
	if err := a.ensureActiveTabRebuildAllowed("provider access"); err != nil {
		return "", err
	}
	keyWarning := ""
	if err := func() error {
		// Keep conflict validation, credential selection, and the config mutation
		// under one config revision. The lock order is config -> credentials.
		unlock := config.LockUserConfigEdits()
		defer unlock()
		cfg, path, err := a.loadDesktopUserConfigForEdit()
		if err != nil {
			return err
		}
		entries, keyEnv, err := officialProviderTemplate(kind, cfg.DeepSeekOfficialPricingLanguage())
		if err != nil {
			return err
		}
		keyEnv, err = validateOfficialProviderAccessInstall(cfg, kind, entries, keyEnv)
		if err != nil {
			return err
		}
		if strings.TrimSpace(key) != "" && keyEnv != "" {
			keyWarning, err = a.saveProviderCredential(keyEnv, key)
			if err != nil {
				return err
			}
		}
		names, err := installOfficialProviderAccess(cfg, kind, entries)
		if err != nil {
			return err
		}
		addProviderAccess(cfg, names...)
		return cfg.SaveTo(path)
	}(); err != nil {
		return "", err
	}
	rebuildWarning := ""
	if err := a.rebuildSetting("provider access"); err != nil {
		var deferred bool
		rebuildWarning, deferred = a.deferredRebuildWarning("provider access", err)
		if !deferred {
			return "", err
		}
	}
	return appendSettingsWarning(keyWarning, rebuildWarning), nil
}

func validateOfficialProviderAccessInstall(c *config.Config, kind string, entries []config.ProviderEntry, fallbackKeyEnv string) (string, error) {
	keyEnv := strings.TrimSpace(fallbackKeyEnv)
	wantKind := strings.ToLower(strings.TrimSpace(kind))
	for _, entry := range entries {
		existing, ok := c.Provider(strings.TrimSpace(entry.Name))
		if !ok {
			continue
		}
		if officialProviderKindFromEntry(*existing) != wantKind {
			return "", fmt.Errorf("official provider %q cannot be added because provider name %q already belongs to a custom endpoint; edit, rename, or remove the existing provider first", kind, entry.Name)
		}
		if env := strings.TrimSpace(existing.APIKeyEnv); env != "" {
			keyEnv = env
		}
	}
	return keyEnv, nil
}

func installOfficialProviderAccess(c *config.Config, kind string, entries []config.ProviderEntry) ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("config is nil")
	}
	wantKind := strings.ToLower(strings.TrimSpace(kind))
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		existing, ok := c.Provider(name)
		if ok {
			if officialProviderKindFromEntry(*existing) != wantKind {
				return nil, fmt.Errorf("official provider %q cannot be added because provider name %q already belongs to a custom endpoint; edit, rename, or remove the existing provider first", kind, name)
			}
			// Re-adding access must not reset customized official transport fields.
			// Only repair an unusable legacy entry that has no model declaration.
			if len(existing.ModelList()) == 0 {
				repaired := repairOfficialProviderCatalog(*existing, entry)
				if err := c.UpsertProvider(repaired); err != nil {
					return nil, err
				}
			}
		} else if err := c.UpsertProvider(entry); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if wantKind == "deepseek" && slices.Contains(names, "deepseek") {
		retargetDeepSeekOfficialReferences(c)
	}
	return names, nil
}

func repairOfficialProviderCatalog(existing, template config.ProviderEntry) config.ProviderEntry {
	existing.Model = ""
	existing.Models = append([]string(nil), template.ModelList()...)
	existing.Default = template.DefaultModel()
	if existing.ContextWindow == 0 {
		existing.ContextWindow = template.ContextWindow
	}
	if existing.MaxOutputTokens == 0 {
		existing.MaxOutputTokens = template.MaxOutputTokens
	}
	if strings.TrimSpace(existing.BalanceURL) == "" {
		existing.BalanceURL = template.BalanceURL
	}
	if strings.TrimSpace(existing.Thinking) == "" {
		existing.Thinking = template.Thinking
	}
	if existing.WebSearch == nil && template.WebSearch != nil {
		enabled := *template.WebSearch
		existing.WebSearch = &enabled
	}
	if existing.Prices == nil {
		existing.Prices = template.Prices
	}
	if existing.Price == nil {
		existing.Price = template.Price
	}
	if existing.ModelOverrides == nil {
		existing.ModelOverrides = map[string]config.ProviderModelOverride{}
	}
	for model, override := range template.ModelOverrides {
		if _, ok := existing.ModelOverrides[model]; !ok {
			existing.ModelOverrides[model] = override
		}
	}
	return existing
}

func retargetDeepSeekOfficialReferences(c *config.Config) {
	if c == nil {
		return
	}
	retarget := func(ref string) string {
		ref = strings.TrimSpace(ref)
		providerName, model, hasModel := strings.Cut(ref, "/")
		switch providerName {
		case "deepseek-flash":
			if !hasModel || strings.TrimSpace(model) == "" {
				model = "deepseek-v4-flash"
			}
		case "deepseek-pro":
			if !hasModel || strings.TrimSpace(model) == "" {
				model = "deepseek-v4-pro"
			}
		default:
			return ref
		}
		return "deepseek/" + model
	}
	c.DefaultModel = retarget(c.DefaultModel)
	c.Agent.PlannerModel = retarget(c.Agent.PlannerModel)
	c.Agent.SubagentModel = retarget(c.Agent.SubagentModel)
	for skillName, ref := range c.Agent.SubagentModels {
		c.Agent.SubagentModels[skillName] = retarget(ref)
	}
}
