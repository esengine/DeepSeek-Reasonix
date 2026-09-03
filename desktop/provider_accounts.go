package main

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
)

type ProviderAccountView struct {
	ProviderID     string   `json:"providerId"`
	AccountID      string   `json:"accountId"`
	PresetID       string   `json:"presetId,omitempty"`
	Label          string   `json:"label"`
	APIKeyEnv      string   `json:"apiKeyEnv"`
	Enabled        bool     `json:"enabled"`
	Default        bool     `json:"default"`
	Retired        bool     `json:"retired,omitempty"`
	KeySet         bool     `json:"keySet"`
	KeySource      string   `json:"keySource,omitempty"`
	KeySourcePath  string   `json:"keySourcePath,omitempty"`
	ProviderNames  []string `json:"providerNames"`
	DisabledRoutes []string `json:"disabledRoutes,omitempty"`
}

func providerPresetViewsForRootWithResolver(cfg *config.Config, root string, resolver *config.CredentialResolver) []ProviderPresetView {
	if resolver == nil {
		resolver = config.NewCredentialResolverForRoot(root)
	}
	presets := config.CuratedProviderPresets()
	out := make([]ProviderPresetView, 0, len(presets))
	for _, preset := range presets {
		keyEnv := strings.TrimSpace(preset.KeyEnv)
		names := make([]string, 0, len(preset.Entries))
		models := make([]string, 0)
		modelSeen := map[string]bool{}
		requiresKey := false
		routes := make([]string, 0, len(preset.Entries))
		for _, entry := range preset.Entries {
			if keyEnv == "" {
				keyEnv = strings.TrimSpace(entry.APIKeyEnv)
			}
			if entry.RequiresAPIKey() {
				requiresKey = true
			}
			name := strings.TrimSpace(entry.Name)
			if name != "" {
				names = append(names, name)
				routes = append(routes, name)
			}
			for _, model := range chatProviderModels(entry.ChatModelList()) {
				if modelSeen[model] {
					continue
				}
				modelSeen[model] = true
				models = append(models, model)
			}
		}
		key := config.CredentialResolution{}
		if keyEnv != "" {
			key = resolver.ResolveGlobalFirst(keyEnv)
		}
		status, statusNames, missingNames := classifyProviderPresetStatus(cfg, preset)
		added := status == providerPresetStatusInstalled || status == providerPresetStatusInstalledModified || status == providerPresetStatusNameConflict
		out = append(out, ProviderPresetView{
			ID: preset.ID, Label: preset.Label, Description: preset.Description, KeyEnv: keyEnv,
			Recommended: preset.Recommended, BillingMode: preset.BillingMode, DisplayGroup: preset.DisplayGroup,
			DisplaySection: preset.DisplaySection, DisplayTier: preset.DisplayTier, RouteKind: preset.RouteKind,
			Optional: preset.Optional, DisplayOrder: preset.DisplayOrder, ProviderNames: nonNil(names),
			Models: nonNil(models), Added: added, Status: status, StatusProviderNames: nonNil(statusNames),
			MissingProviderNames: nonNil(missingNames), KeySet: key.Set, RequiresKey: requiresKey,
			Configured: !requiresKey || key.Set, KeySource: key.Source.Label, KeySourcePath: key.Source.Path,
			AccountGroupID: preset.AccountGroupID, Accounts: accountViewsForGroup(cfg, preset.AccountGroupID, root, resolver), CanAddAccount: true,
			AvailableRoutes: nonNil(routes),
		})
	}
	return out
}

func accountViewsForGroup(cfg *config.Config, groupID, root string, resolver *config.CredentialResolver) []ProviderAccountView {
	groupID = strings.TrimSpace(groupID)
	out := make([]ProviderAccountView, 0)
	for _, view := range providerAccountViewsForRoot(cfg, root, resolver) {
		if view.ProviderID == groupID {
			out = append(out, view)
		}
	}
	return out
}

func providerAccountViewsForRoot(cfg *config.Config, root string, resolver *config.CredentialResolver) []ProviderAccountView {
	out := make([]ProviderAccountView, 0)
	if cfg == nil {
		return out
	}
	if resolver == nil {
		resolver = config.NewCredentialResolverForRoot(root)
	}
	for _, account := range cfg.ProviderAccounts {
		view := ProviderAccountView{
			ProviderID:     account.ProviderID,
			AccountID:      account.ID,
			PresetID:       account.PresetID,
			Label:          account.Label,
			APIKeyEnv:      account.APIKeyEnv,
			Enabled:        account.IsEnabled(),
			Default:        account.Default,
			ProviderNames:  []string{},
			DisabledRoutes: nonNil(account.DisabledRoutes),
		}
		if env := strings.TrimSpace(account.APIKeyEnv); env != "" {
			key := resolver.ResolveGlobalFirst(env)
			view.KeySet = key.Set
			view.KeySource = key.Source.Label
			view.KeySourcePath = key.Source.Path
		}
		if entries, ok := cfg.ResolveAccountProvider(account.ProviderID, account.ID); ok {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name)
			}
			view.ProviderNames = nonNil(names)
		}
		out = append(out, view)
	}
	return out
}

func applyAccountMetadataToProviderView(view *ProviderView, p config.ProviderEntry, cfg *config.Config) {
	if view == nil {
		return
	}
	view.ProviderID = p.AccountProviderID
	view.AccountID = p.AccountID
	view.AccountLabel = p.AccountLabel
	if account, ok := config.ProviderAccountForEntry(cfg, p); ok {
		view.ProviderID = account.ProviderID
		view.AccountID = account.ID
		view.AccountLabel = account.Label
		view.AccountEnabled = account.IsEnabled()
		view.AccountDefault = account.Default
	}
}

func (a *App) AddProviderPresetAccount(presetID, label, key string) (string, error) {
	preset, ok := config.CuratedProviderPreset(presetID)
	if !ok {
		return "", fmt.Errorf("unknown provider preset %q", presetID)
	}
	groupID := preset.AccountGroupID
	if groupID == "" {
		groupID = preset.ID
	}
	if err := a.ensureActiveTabRebuildAllowed("provider account"); err != nil {
		return "", err
	}
	keyWarning := ""
	var created config.ProviderAccount
	change, err := a.applyConfigChangeResult("provider account", func(c *config.Config) error {
		env := ""
		account, err := c.AddProviderAccount(groupID, preset.ID, label, env)
		if err != nil {
			return err
		}
		created = account
		entries, _ := c.ResolveAccountProvider(account.ProviderID, account.ID)
		addProviderAccess(c, providerEntryNames(entries)...)
		return nil
	})
	if err != nil {
		if !change.Committed && strings.TrimSpace(created.APIKeyEnv) != "" && strings.TrimSpace(key) != "" {
			_ = config.RemoveCredential(created.APIKeyEnv)
		}
		return "", err
	}
	if strings.TrimSpace(key) != "" && strings.TrimSpace(created.APIKeyEnv) != "" {
		keyWarning, err = a.saveProviderCredential(created.APIKeyEnv, key)
		if err != nil {
			return "", fmt.Errorf("configuration saved but credential write failed; retry from account settings: %w", err)
		}
	}
	return appendSettingsWarning(keyWarning, change.Warning), nil
}

func (a *App) SetProviderAccountDefault(providerID, accountID string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		return c.SetProviderAccountDefault(providerID, accountID)
	})
}

func (a *App) SetProviderAccountEnabled(providerID, accountID string, enabled bool) error {
	return a.applyConfigChange(func(c *config.Config) error {
		return c.SetProviderAccountEnabled(providerID, accountID, enabled)
	})
}

func (a *App) RenameProviderAccount(providerID, accountID, label string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		return c.RenameProviderAccount(providerID, accountID, label)
	})
}

func (a *App) RetireProviderAccount(providerID, accountID string) error {
	var retiredEnv string
	_, err := a.applyConfigChangeWithRuntimeMutation("retire provider account", func(c *config.Config) error {
		if refs := a.providerAccountLiveRefsFromConfig(c, providerID, accountID); len(refs) > 0 {
			return fmt.Errorf("cannot retire account %s/%s while it is referenced by %s", providerID, accountID, strings.Join(refs, ", "))
		}
		if _, account, ok := accountByID(c, providerID, accountID); ok {
			retiredEnv = strings.TrimSpace(account.APIKeyEnv)
		}
		if err := c.RetireProviderAccount(providerID, accountID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if retiredEnv != "" {
		if cfg, _, loadErr := a.loadDesktopUserConfigForView(); loadErr == nil {
			if _, account, ok := accountByID(cfg, providerID, accountID); ok && !accountKeyEnvShared(cfg, account) {
				if removeErr := config.RemoveCredential(retiredEnv); removeErr != nil {
					return fmt.Errorf("account retired but credential cleanup failed; retry cleanup: %w", removeErr)
				}
			}
		}
	}
	return err
}

// RestoreProviderAccount re-enables a retired account and all of its routes.
func (a *App) RestoreProviderAccount(providerID, accountID string) error {
	_, err := a.applyConfigChangeWithRuntimeMutation("restore provider account", func(c *config.Config) error {
		return c.RestoreProviderAccount(providerID, accountID)
	})
	return err
}

// SetProviderAccountRouteEnabled persists a single route toggle while keeping
// account-owned provider entries available for old session references.
func (a *App) SetProviderAccountRouteEnabled(providerID, accountID, routeID string, enabled bool) error {
	_, err := a.applyConfigChangeWithRuntimeMutation("provider account route", func(c *config.Config) error {
		return c.SetProviderAccountRouteEnabled(providerID, accountID, routeID, enabled)
	})
	return err
}

func (a *App) SetProviderAccountKey(providerID, accountID, value string) (string, error) {
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return "", err
	}
	_, account, ok := accountByID(cfg, providerID, accountID)
	if !ok {
		return "", fmt.Errorf("set account key: no account %s/%s", providerID, accountID)
	}
	if strings.TrimSpace(account.APIKeyEnv) == "" {
		return "", fmt.Errorf("set account key: account %s/%s has no api_key_env", providerID, accountID)
	}
	return a.SetProviderKey(account.APIKeyEnv, value)
}

func (a *App) ClearProviderAccountKey(providerID, accountID string) error {
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return err
	}
	_, account, ok := accountByID(cfg, providerID, accountID)
	if !ok {
		return fmt.Errorf("clear account key: no account %s/%s", providerID, accountID)
	}
	return a.ClearProviderKey(account.APIKeyEnv)
}

func (a *App) providerAccountLiveRefs(providerID, accountID string) []string {
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return nil
	}
	return a.providerAccountLiveRefsFromConfig(cfg, providerID, accountID)
}

func (a *App) providerAccountLiveRefsFromConfig(cfg *config.Config, providerID, accountID string) []string {
	refs := cfg.ProviderAccountConfigRefs(providerID, accountID)
	entries, ok := cfg.ResolveAccountProvider(providerID, accountID)
	if !ok {
		return refs
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tab := range a.tabs {
		if tab == nil {
			continue
		}
		entry, found := cfg.ResolveModel(tab.model)
		if found && names[entry.Name] {
			refs = append(refs, "tab:"+tab.ID)
		}
	}
	for _, tab := range a.detachedSessions {
		if tab == nil {
			continue
		}
		entry, found := cfg.ResolveModel(tab.model)
		if found && names[entry.Name] {
			refs = append(refs, "background:"+tab.ID)
		}
	}
	return refs
}

func accountKeyEnvShared(c *config.Config, account config.ProviderAccount) bool {
	if c == nil {
		return false
	}
	env := strings.TrimSpace(account.APIKeyEnv)
	if env == "" {
		return false
	}
	for _, other := range c.ProviderAccounts {
		if other.ProviderID == account.ProviderID && other.ID == account.ID {
			continue
		}
		if strings.TrimSpace(other.APIKeyEnv) == env {
			return true
		}
	}
	// Provider entries can outlive an account (retired routes are retained for
	// old sessions), and custom providers may intentionally share the same key.
	for _, entry := range c.Providers {
		if strings.TrimSpace(entry.APIKeyEnv) != env {
			continue
		}
		if entry.AccountProviderID == account.ProviderID && entry.AccountID == account.ID {
			continue
		}
		return true
	}
	return false
}

func accountByID(c *config.Config, providerID, accountID string) (int, config.ProviderAccount, bool) {
	if c == nil {
		return -1, config.ProviderAccount{}, false
	}
	for i, account := range c.ProviderAccounts {
		if account.ProviderID == providerID && account.ID == accountID {
			return i, account, true
		}
	}
	return -1, config.ProviderAccount{}, false
}

func providerEntryNames(entries []config.ProviderEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if name := strings.TrimSpace(e.Name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func providerThinkingForSettings(thinking string) string {
	normalized := strings.ToLower(strings.TrimSpace(thinking))
	switch normalized {
	case "enabled", "disabled", "adaptive":
		return normalized
	default:
		return ""
	}
}
