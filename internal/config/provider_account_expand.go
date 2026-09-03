package config

import (
	"fmt"
	"sort"
	"strings"
)

func ExpandProviderAccount(c *Config, account ProviderAccount) ([]ProviderEntry, error) {
	if err := validateProviderAccount(account); err != nil {
		return nil, err
	}
	templates := accountRouteTemplates(account.ProviderID)
	if len(templates) == 0 {
		return nil, fmt.Errorf("unknown provider family %q", account.ProviderID)
	}
	taken := map[string]bool{}
	if c != nil {
		for _, p := range c.Providers {
			if p.AccountProviderID == account.ProviderID && p.AccountID == account.ID {
				continue
			}
			if name := strings.TrimSpace(p.Name); name != "" {
				taken[name] = true
			}
		}
	}
	out := make([]ProviderEntry, 0, len(templates))
	for _, tmpl := range templates {
		if providerAccountRouteDisabled(account, tmpl.RouteID) {
			continue
		}
		if tmpl.MainOnly && account.ID != MainProviderAccountID {
			continue
		}
		if tmpl.ExtraOnly && account.ID == MainProviderAccountID {
			continue
		}
		if strings.TrimSpace(tmpl.Entry.Name) == "" && strings.TrimSpace(tmpl.Entry.Kind) == "" {
			continue
		}
		if tmpl.Optional && !accountWantsOptionalRoute(c, account, tmpl) {
			continue
		}
		existing := findAccountRouteEntry(c, account, tmpl.RouteID)
		name := tmpl.BaseName
		if account.ID != MainProviderAccountID || tmpl.ExtraOnly {
			name = tmpl.BaseName + "--" + account.ID
		}
		if existing != nil {
			name = existing.Name
		} else {
			name = uniqueProviderName(name, taken)
		}
		taken[name] = true
		var entry ProviderEntry
		if existing != nil {
			entry = cloneProviderEntry(*existing)
			if strings.TrimSpace(entry.APIKeyEnv) == "" {
				entry.APIKeyEnv = account.APIKeyEnv
			}
		} else {
			entry = cloneProviderEntry(tmpl.Entry)
			entry.Name = name
			entry.APIKeyEnv = account.APIKeyEnv
			if preset := strings.TrimSpace(account.PresetID); preset != "" {
				entry.PresetID = preset
				entry.PresetVersion = ProviderPresetVersion
			}
		}
		stampAccountMetadata(&entry, account, tmpl.RouteID)
		out = append(out, entry)
	}
	return out, nil
}

// MaterializeProviderAccount is the stable account-to-runtime projection used
// by new callers. ExpandProviderAccount remains as a compatibility alias.
func MaterializeProviderAccount(c *Config, account ProviderAccount) ([]ProviderEntry, error) {
	return ExpandProviderAccount(c, account)
}

func ReconcileProviderAccounts(c *Config) (changed bool, warnings []string, err error) {
	if c == nil {
		return false, nil, nil
	}
	warnings = append(warnings, normalizeProviderAccountList(c)...)
	if inferred := inferProviderAccounts(c); inferred {
		changed = true
	}
	if attached := attachOrphanCuratedProviders(c); attached {
		changed = true
	}
	for _, account := range c.ProviderAccounts {
		if account.Retired {
			continue
		}
		entries, expandErr := MaterializeProviderAccount(c, account)
		if expandErr != nil {
			warnings = append(warnings, expandErr.Error())
			continue
		}
		for _, generated := range entries {
			if !c.userOwnedProvider(generated.Name) && providerIndexByName(c, generated.Name) >= 0 {
				continue
			}
			idx := indexAccountRouteEntry(c, account, generated.AccountRouteID)
			if idx < 0 {
				idx = providerIndexByName(c, generated.Name)
			}
			if idx >= 0 {
				merged := preserveUserProviderFields(c.Providers[idx], generated)
				if !ProviderEntriesConfigEqual(c.Providers[idx], merged) {
					c.Providers[idx] = merged
					changed = true
				}
				continue
			}
			if err := c.UpsertProvider(generated); err != nil {
				return changed, warnings, err
			}
			c.markUserProvider(generated.Name)
			changed = true
		}
	}
	return changed, warnings, nil
}

func ProviderAccountForEntry(c *Config, e ProviderEntry) (ProviderAccount, bool) {
	providerID, accountID, ok := ProviderAccountIdentity(e)
	if !ok {
		if group, route, _, found := curatedProviderIdentity(e); found {
			providerID, accountID = group, MainProviderAccountID
			_ = route
			ok = true
		}
	}
	if !ok {
		return ProviderAccount{}, false
	}
	if c != nil {
		if _, account, found := c.lookupProviderAccount(providerID, accountID); found {
			return cloneProviderAccount(account), true
		}
	}
	label := strings.TrimSpace(e.AccountLabel)
	if label == "" {
		label = defaultAccountLabel(accountID)
	}
	return ProviderAccount{
		ProviderID: providerID,
		ID:         accountID,
		Label:      label,
		APIKeyEnv:  e.APIKeyEnv,
	}, true
}

func ProviderAccountIdentity(e ProviderEntry) (providerID, accountID string, ok bool) {
	providerID = strings.TrimSpace(e.AccountProviderID)
	accountID = strings.TrimSpace(e.AccountID)
	if providerID == "" || accountID == "" {
		return "", "", false
	}
	return providerID, accountID, true
}

func preserveUserProviderFields(existing, generated ProviderEntry) ProviderEntry {
	out := cloneProviderEntry(existing)
	if strings.TrimSpace(out.AccountProviderID) == "" {
		out.AccountProviderID = generated.AccountProviderID
	}
	if strings.TrimSpace(out.AccountID) == "" {
		out.AccountID = generated.AccountID
	}
	if strings.TrimSpace(out.AccountRouteID) == "" {
		out.AccountRouteID = generated.AccountRouteID
	}
	if strings.TrimSpace(out.AccountLabel) == "" || (out.AccountProviderID == generated.AccountProviderID && out.AccountID == generated.AccountID) {
		out.AccountLabel = generated.AccountLabel
	}
	if strings.TrimSpace(out.APIKeyEnv) == "" {
		out.APIKeyEnv = generated.APIKeyEnv
	}
	return out
}

func accountWantsOptionalRoute(c *Config, account ProviderAccount, tmpl accountRouteTemplate) bool {
	if findAccountRouteEntry(c, account, tmpl.RouteID) != nil {
		return true
	}
	presetID := strings.TrimSpace(account.PresetID)
	if presetID == "" || c == nil {
		return false
	}
	preset, ok := CuratedProviderPreset(presetID)
	if !ok {
		return false
	}
	for _, e := range preset.Entries {
		if strings.TrimSpace(e.Name) == tmpl.BaseName || strings.TrimSpace(e.Name) == tmpl.RouteID {
			return true
		}
	}
	return false
}

func findAccountRouteEntry(c *Config, account ProviderAccount, routeID string) *ProviderEntry {
	idx := indexAccountRouteEntry(c, account, routeID)
	if idx < 0 {
		return nil
	}
	return &c.Providers[idx]
}

func indexAccountRouteEntry(c *Config, account ProviderAccount, routeID string) int {
	if c == nil {
		return -1
	}
	routeID = strings.TrimSpace(routeID)
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.AccountProviderID == account.ProviderID && p.AccountID == account.ID && strings.TrimSpace(p.AccountRouteID) == routeID {
			return i
		}
	}
	if account.ID == MainProviderAccountID {
		for i := range c.Providers {
			p := &c.Providers[i]
			if strings.TrimSpace(p.AccountProviderID) != "" {
				continue
			}
			if group, route, _, ok := curatedProviderIdentity(*p); ok && group == account.ProviderID && route == routeID {
				return i
			}
		}
	}
	return -1
}

func providerIndexByName(c *Config, name string) int {
	if c == nil {
		return -1
	}
	name = strings.TrimSpace(name)
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			return i
		}
	}
	return -1
}

func normalizeProviderAccountList(c *Config) []string {
	if c == nil {
		return nil
	}
	var warnings []string
	seen := map[providerAccountKey]int{}
	defaults := map[string]int{}
	out := c.ProviderAccounts[:0]
	for _, account := range c.ProviderAccounts {
		account.ProviderID = strings.TrimSpace(account.ProviderID)
		account.ID = strings.TrimSpace(account.ID)
		account.Label = strings.TrimSpace(account.Label)
		account.APIKeyEnv = strings.TrimSpace(account.APIKeyEnv)
		account.PresetID = strings.TrimSpace(account.PresetID)
		account.DisabledRoutes = normalizeProviderAccountRoutes(account.DisabledRoutes)
		if err := validateProviderAccount(account); err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		key := account.key()
		if prev, dup := seen[key]; dup {
			warnings = append(warnings, fmt.Sprintf("duplicate provider account %s/%s; keeping the first declaration", account.ProviderID, account.ID))
			_ = prev
			continue
		}
		if account.Default {
			if prev, exists := defaults[account.ProviderID]; exists {
				warnings = append(warnings, fmt.Sprintf("multiple default accounts for %s; keeping %s", account.ProviderID, c.ProviderAccounts[prev].ID))
				account.Default = false
			} else {
				defaults[account.ProviderID] = len(out)
			}
		}
		seen[key] = len(out)
		out = append(out, account)
	}
	if len(out) == 0 {
		c.ProviderAccounts = nil
	} else {
		c.ProviderAccounts = out
	}
	return warnings
}

func normalizeProviderAccountRoutes(routes []string) []string {
	if len(routes) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(routes))
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		route = strings.TrimSpace(route)
		if route == "" {
			continue
		}
		if _, ok := seen[route]; ok {
			continue
		}
		seen[route] = struct{}{}
		out = append(out, route)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func providerAccountRouteDisabled(account ProviderAccount, routeID string) bool {
	routeID = strings.TrimSpace(routeID)
	for _, disabled := range account.DisabledRoutes {
		if strings.TrimSpace(disabled) == routeID {
			return true
		}
	}
	return false
}
