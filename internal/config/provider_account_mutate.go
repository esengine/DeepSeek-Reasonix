package config

import (
	"fmt"
	"slices"
	"strings"
)

func (c *Config) AddProviderAccount(providerID, presetID, label, apiKeyEnv string) (ProviderAccount, error) {
	if c == nil {
		return ProviderAccount{}, fmt.Errorf("add provider account: nil config")
	}
	ensureProviderAccounts(c)
	providerID = strings.TrimSpace(providerID)
	presetID = strings.TrimSpace(presetID)
	label = strings.TrimSpace(label)
	apiKeyEnv = strings.TrimSpace(apiKeyEnv)
	if providerID == "" && presetID != "" {
		if preset, ok := CuratedProviderPreset(presetID); ok {
			providerID = preset.resolvedAccountGroupID()
		} else {
			providerID = accountGroupIDForPresetID(presetID)
		}
	}
	if providerID == "" {
		return ProviderAccount{}, fmt.Errorf("provider account: provider_id is required")
	}
	if len(accountRouteTemplates(providerID)) == 0 {
		return ProviderAccount{}, fmt.Errorf("unknown provider family %q", providerID)
	}
	if label == "" {
		label = defaultAccountLabel(MainProviderAccountID)
	}
	usedIDs := c.providerAccountUsedIDs()
	id := SuggestProviderAccountID(providerID, label)
	if !c.hasProviderFamilyAccount(providerID) {
		id = MainProviderAccountID
	}
	id = uniqueProviderAccountID(providerID, id, usedIDs)
	if apiKeyEnv == "" {
		apiKeyEnv = SuggestAccountAPIKeyEnv(baseAPIKeyEnvForGroup(providerID), id, c.usedAPIKeyEnvs())
	}
	account := ProviderAccount{
		ProviderID: providerID,
		PresetID:   presetID,
		ID:         id,
		Label:      label,
		APIKeyEnv:  apiKeyEnv,
		Default:    !c.hasProviderFamilyDefault(providerID),
	}
	if err := validateProviderAccount(account); err != nil {
		return ProviderAccount{}, err
	}
	change := ProviderAccountChange{FamilyID: providerID, AccountID: account.ID, After: &account}
	if err := c.ApplyProviderAccountChange(change); err != nil {
		return ProviderAccount{}, err
	}
	return account, nil
}

func (c *Config) SetProviderAccountDefault(providerID, accountID string) error {
	_, account, ok := c.lookupProviderAccount(providerID, accountID)
	if !ok {
		return fmt.Errorf("set default account: no account %s/%s", providerID, accountID)
	}
	if account.Retired || !account.IsEnabled() {
		return fmt.Errorf("set default account: %s/%s is not available", providerID, accountID)
	}
	after := cloneProviderAccount(account)
	after.Default = true
	return c.ApplyProviderAccountChange(ProviderAccountChange{FamilyID: account.ProviderID, AccountID: account.ID, Before: &account, After: &after, syncDefaultModel: true})
}

func (c *Config) SetProviderAccountEnabled(providerID, accountID string, enabled bool) error {
	_, account, ok := c.lookupProviderAccount(providerID, accountID)
	if !ok {
		return fmt.Errorf("set account enabled: no account %s/%s", providerID, accountID)
	}
	if account.Retired {
		return fmt.Errorf("set account enabled: %s/%s is retired", providerID, accountID)
	}
	after := cloneProviderAccount(account)
	after.Enabled = boolPointer(enabled)
	if !enabled {
		after.Default = false
	}
	return c.ApplyProviderAccountChange(ProviderAccountChange{FamilyID: account.ProviderID, AccountID: account.ID, Before: &account, After: &after, syncDefaultModel: true})
}

// SetProviderAccountRouteEnabled toggles a generated route for new selection;
// retained entries keep old sessions resolvable.
func (c *Config) SetProviderAccountRouteEnabled(providerID, accountID, routeID string, enabled bool) error {
	if c == nil {
		return fmt.Errorf("set account route: nil config")
	}
	idx, account, ok := c.lookupProviderAccount(providerID, accountID)
	if !ok {
		return fmt.Errorf("set account route: no account %s/%s", providerID, accountID)
	}
	if account.Retired {
		return fmt.Errorf("set account route: %s/%s is retired", providerID, accountID)
	}
	routeID = strings.TrimSpace(routeID)
	if routeID == "" {
		return fmt.Errorf("set account route: route_id is required")
	}
	known := false
	for _, tmpl := range accountRouteTemplates(account.ProviderID) {
		if tmpl.RouteID == routeID {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("set account route: unknown route %q for provider %s", routeID, providerID)
	}
	disabled := normalizeProviderAccountRoutes(account.DisabledRoutes)
	oldDisabled := append([]string(nil), disabled...)
	filtered := disabled[:0]
	for _, route := range disabled {
		if route != routeID {
			filtered = append(filtered, route)
		}
	}
	if !enabled {
		filtered = append(filtered, routeID)
	}
	c.ProviderAccounts[idx].DisabledRoutes = normalizeProviderAccountRoutes(filtered)
	if _, _, err := ReconcileProviderAccounts(c); err != nil {
		c.ProviderAccounts[idx].DisabledRoutes = oldDisabled
		return err
	}
	if enabled && indexAccountRouteEntry(c, account, routeID) < 0 {
		// Explicitly enabling an optional route opts into its curated preset route.
		if err := ensureProviderAccountRoute(c, c.ProviderAccounts[idx], routeID); err != nil {
			c.ProviderAccounts[idx].DisabledRoutes = oldDisabled
			return err
		}
	}
	if !enabled {
		c.removeProviderAccountRouteAccess(account.ProviderID, account.ID, routeID)
	} else {
		c.restoreProviderAccountRouteAccess(account.ProviderID, account.ID, routeID)
	}
	return nil
}

func ensureProviderAccountRoute(c *Config, account ProviderAccount, routeID string) error {
	for _, tmpl := range accountRouteTemplates(account.ProviderID) {
		if tmpl.RouteID != routeID {
			continue
		}
		candidate := account
		if tmpl.Optional && strings.TrimSpace(candidate.PresetID) == "" {
			for _, preset := range curatedProviderPresets {
				if accountGroupIDForPresetID(preset.ID) != account.ProviderID {
					continue
				}
				for _, entry := range preset.Entries {
					if strings.TrimSpace(entry.Name) == tmpl.BaseName || strings.TrimSpace(entry.Name) == tmpl.RouteID {
						candidate.PresetID = preset.ID
						break
					}
				}
				if candidate.PresetID != "" {
					break
				}
			}
		}
		entries, err := MaterializeProviderAccount(c, candidate)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.AccountRouteID != routeID {
				continue
			}
			if err := c.UpsertProvider(entry); err != nil {
				return err
			}
			c.markUserProvider(entry.Name)
			return nil
		}
		return fmt.Errorf("set account route: route %q is unavailable in preset %q", routeID, candidate.PresetID)
	}
	return fmt.Errorf("set account route: unknown route %q for provider %s", routeID, account.ProviderID)
}

// RestoreProviderAccount re-enables an account, clears disabled routes and
// recreates any missing generated provider entries. Existing provider names
// and user customizations are preserved by reconciliation.
func (c *Config) RestoreProviderAccount(providerID, accountID string) error {
	if c == nil {
		return fmt.Errorf("restore account: nil config")
	}
	idx, account, ok := c.lookupProviderAccount(providerID, accountID)
	if !ok {
		return fmt.Errorf("restore account: no account %s/%s", providerID, accountID)
	}
	c.ProviderAccounts[idx].Retired = false
	c.ProviderAccounts[idx].Enabled = boolPointer(true)
	c.ProviderAccounts[idx].DisabledRoutes = nil
	if !c.hasProviderFamilyDefault(providerID) {
		c.ProviderAccounts[idx].Default = true
	}
	entries, err := MaterializeProviderAccount(c, c.ProviderAccounts[idx])
	if err != nil {
		c.ProviderAccounts[idx] = account
		return err
	}
	for _, generated := range entries {
		if err := c.UpsertProvider(generated); err != nil {
			c.ProviderAccounts[idx] = account
			return err
		}
		c.markUserProvider(generated.Name)
	}
	if _, _, err := ReconcileProviderAccounts(c); err != nil {
		c.ProviderAccounts[idx] = account
		return err
	}
	if c.Desktop.ProviderAccess != nil {
		for _, entry := range c.Providers {
			if entry.AccountProviderID == providerID && entry.AccountID == accountID {
				c.restoreProviderAccountRouteAccess(providerID, accountID, entry.AccountRouteID)
			}
		}
	}
	return nil
}

func (c *Config) removeProviderAccountRouteAccess(providerID, accountID, routeID string) {
	if c == nil || c.Desktop.ProviderAccess == nil {
		return
	}
	names := map[string]bool{}
	for _, entry := range c.Providers {
		if entry.AccountProviderID == providerID && entry.AccountID == accountID && strings.TrimSpace(entry.AccountRouteID) == routeID {
			names[entry.Name] = true
		}
	}
	if len(names) == 0 {
		return
	}
	out := c.Desktop.ProviderAccess[:0]
	for _, name := range c.Desktop.ProviderAccess {
		if !names[strings.TrimSpace(name)] {
			out = append(out, name)
		}
	}
	c.Desktop.ProviderAccess = out
}

func (c *Config) restoreProviderAccountRouteAccess(providerID, accountID, routeID string) {
	if c == nil || c.Desktop.ProviderAccess == nil {
		return
	}
	for _, entry := range c.Providers {
		if entry.AccountProviderID != providerID || entry.AccountID != accountID || strings.TrimSpace(entry.AccountRouteID) != strings.TrimSpace(routeID) {
			continue
		}
		present := false
		present = slices.Contains(c.Desktop.ProviderAccess, entry.Name)
		if present {
			continue
		}
		c.Desktop.ProviderAccess = append(c.Desktop.ProviderAccess, entry.Name)
		break
	}
}

func (c *Config) RenameProviderAccount(providerID, accountID, label string) error {
	_, account, ok := c.lookupProviderAccount(providerID, accountID)
	if !ok {
		return fmt.Errorf("rename account: no account %s/%s", providerID, accountID)
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Errorf("rename account: label is required")
	}
	after := cloneProviderAccount(account)
	after.Label = label
	return c.ApplyProviderAccountChange(ProviderAccountChange{FamilyID: account.ProviderID, AccountID: account.ID, Before: &account, After: &after})
}

func (c *Config) RetireProviderAccount(providerID, accountID string) error {
	_, account, ok := c.lookupProviderAccount(providerID, accountID)
	if !ok {
		return fmt.Errorf("retire account: no account %s/%s", providerID, accountID)
	}
	if refs := c.ProviderAccountConfigRefs(providerID, accountID); len(refs) > 0 {
		return fmt.Errorf("retire account %s/%s: still referenced by %s", providerID, accountID, strings.Join(refs, ", "))
	}
	after := cloneProviderAccount(account)
	after.Retired = true
	after.Enabled = boolPointer(false)
	after.Default = false
	for _, tmpl := range accountRouteTemplates(account.ProviderID) {
		after.DisabledRoutes = append(after.DisabledRoutes, tmpl.RouteID)
	}
	after.DisabledRoutes = normalizeProviderAccountRoutes(after.DisabledRoutes)
	return c.ApplyProviderAccountChange(ProviderAccountChange{FamilyID: account.ProviderID, AccountID: account.ID, Before: &account, After: &after, syncDefaultModel: true})
}

func (c *Config) SetProviderAccountKeyEnv(providerID, accountID, apiKeyEnv string) error {
	idx, account, ok := c.lookupProviderAccount(providerID, accountID)
	if !ok {
		return fmt.Errorf("set account key: no account %s/%s", providerID, accountID)
	}
	apiKeyEnv = strings.TrimSpace(apiKeyEnv)
	if apiKeyEnv == "" || !IsValidCredentialKey(apiKeyEnv) {
		return fmt.Errorf("set account key: api_key_env %q is not a valid environment variable name", apiKeyEnv)
	}
	c.ProviderAccounts[idx].APIKeyEnv = apiKeyEnv
	for i := range c.Providers {
		if c.Providers[i].AccountProviderID == account.ProviderID && c.Providers[i].AccountID == account.ID {
			c.Providers[i].APIKeyEnv = apiKeyEnv
		}
	}
	return nil
}

func (c *Config) ProviderAccountConfigRefs(providerID, accountID string) []string {
	entries, ok := c.ResolveAccountProvider(providerID, accountID)
	if !ok {
		return nil
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}
	var refs []string
	add := func(field, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		entry, found := c.ResolveModel(value)
		if !found || !names[entry.Name] {
			return
		}
		refs = append(refs, field)
	}
	add("default_model", c.DefaultModel)
	add("agent.planner_model", c.Agent.PlannerModel)
	add("agent.vision_model", c.Agent.VisionModel)
	add("agent.subagent_model", c.Agent.SubagentModel)
	add("agent.guardian_model", c.Agent.GuardianModel)
	add("agent.recovery_model", c.Agent.RecoveryModel)
	for skill, ref := range c.Agent.SubagentModels {
		add("agent.subagent_models."+skill, ref)
	}
	add("bot.model", c.Bot.Model)
	for _, conn := range c.Bot.Connections {
		add("bot.connections."+conn.ID, conn.Model)
	}
	return refs
}

func (c *Config) hasProviderFamilyAccount(providerID string) bool {
	for _, account := range c.ProviderAccounts {
		if account.ProviderID == providerID && !account.Retired {
			return true
		}
	}
	return false
}

func (c *Config) hasProviderFamilyDefault(providerID string) bool {
	for _, account := range c.ProviderAccounts {
		if account.ProviderID == providerID && account.Default && account.IsEnabled() {
			return true
		}
	}
	return false
}

func (c *Config) syncFamilyDefaultModel(providerID, accountID string) {
	if c == nil {
		return
	}
	current := strings.TrimSpace(c.DefaultModel)
	if current == "" {
		return
	}
	entry, ok := c.ResolveModel(current)
	if !ok {
		return
	}
	family, _, ok := ProviderAccountIdentity(*entry)
	if !ok {
		family, _, _, ok = curatedProviderIdentity(*entry)
	}
	if !ok || family != providerID {
		return
	}
	_, target, ok := c.lookupProviderAccount(providerID, accountID)
	if !ok || !target.IsEnabled() {
		return
	}
	entries, ok := c.ResolveAccountProvider(providerID, accountID)
	if !ok {
		return
	}
	model := entry.Model
	for _, candidate := range entries {
		if candidate.HasModel(model) && c.accountSelectable(candidate) {
			c.DefaultModel = candidate.Name + "/" + model
			return
		}
	}
	for _, candidate := range entries {
		if !c.accountSelectable(candidate) {
			continue
		}
		models := candidate.ChatModelList()
		if len(models) == 0 {
			continue
		}
		selected := candidate.DefaultModel()
		if !candidate.HasModel(selected) {
			selected = models[0]
		}
		c.DefaultModel = candidate.Name + "/" + selected
		return
	}
}

func (c *Config) SetProviderEffort(name, effort string) error {
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			c.Providers[i].Effort = normalizeStoredEffort(effort)
			return nil
		}
	}
	return fmt.Errorf("set provider effort: no provider %q", name)
}
