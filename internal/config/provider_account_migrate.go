package config

import "strings"

func ensureProviderAccounts(c *Config) {
	if c == nil {
		return
	}
	_, warnings, err := ReconcileProviderAccounts(c)
	if err != nil {
		c.addLoadWarning(err.Error())
	}
	for _, warning := range warnings {
		c.addLoadWarning(warning)
	}
}

func (c *Config) prepareUserPersist() {
	if c == nil {
		return
	}
	ensureProviderAccounts(c)
}

func inferProviderAccounts(c *Config) bool {
	if c == nil || len(c.ProviderAccounts) > 0 {
		return false
	}
	type group struct {
		family string
		keyEnv string
		index  []int
	}
	order := make([]group, 0, 4)
	indexOf := map[string]int{}
	for i := range c.Providers {
		p := c.Providers[i]
		if !c.userOwnedProvider(p.Name) {
			continue
		}
		family, _, _, ok := curatedProviderIdentity(p)
		if !ok {
			continue
		}
		key := family + "\x00" + strings.TrimSpace(p.APIKeyEnv)
		if idx, exists := indexOf[key]; exists {
			order[idx].index = append(order[idx].index, i)
			continue
		}
		indexOf[key] = len(order)
		order = append(order, group{family: family, keyEnv: strings.TrimSpace(p.APIKeyEnv), index: []int{i}})
	}
	if len(order) == 0 {
		return false
	}
	usedIDs := c.providerAccountUsedIDs()
	firstFamily := map[string]bool{}
	changed := false
	for _, g := range order {
		id := MainProviderAccountID
		label := defaultAccountLabel(id)
		if firstFamily[g.family] {
			id = legacyAccountID(g.keyEnv, g.family, usedIDs)
			label = defaultAccountLabel(id)
		}
		firstFamily[g.family] = true
		presetID := inferAccountPresetID(c, g.index)
		account := ProviderAccount{
			ProviderID: g.family,
			PresetID:   presetID,
			ID:         id,
			Label:      label,
			APIKeyEnv:  g.keyEnv,
			Default:    id == MainProviderAccountID,
		}
		// Preserve sparse legacy route declarations; restore can opt into the full bundle.
		routePresent := map[string]bool{}
		for _, idx := range g.index {
			if _, route, _, ok := curatedProviderIdentity(c.Providers[idx]); ok && route != "" {
				routePresent[route] = true
			} else {
				routePresent[strings.TrimSpace(c.Providers[idx].Name)] = true
			}
		}
		for _, tmpl := range accountRouteTemplates(g.family) {
			if !routePresent[tmpl.RouteID] {
				account.DisabledRoutes = append(account.DisabledRoutes, tmpl.RouteID)
			}
		}
		account.DisabledRoutes = normalizeProviderAccountRoutes(account.DisabledRoutes)
		if account.APIKeyEnv == "" {
			account.APIKeyEnv = baseAPIKeyEnvForGroup(g.family)
		}
		c.ProviderAccounts = append(c.ProviderAccounts, account)
		usedIDs[account.key()] = true
		for _, idx := range g.index {
			routeID := strings.TrimSpace(c.Providers[idx].Name)
			if _, route, _, ok := curatedProviderIdentity(c.Providers[idx]); ok && route != "" {
				routeID = route
			}
			stampAccountMetadata(&c.Providers[idx], account, routeID)
		}
		changed = true
	}
	return changed
}

func attachOrphanCuratedProviders(c *Config) bool {
	if c == nil {
		return false
	}
	changed := false
	usedIDs := c.providerAccountUsedIDs()
	for i := range c.Providers {
		p := &c.Providers[i]
		if strings.TrimSpace(p.AccountProviderID) != "" && strings.TrimSpace(p.AccountID) != "" {
			continue
		}
		if !c.userOwnedProvider(p.Name) {
			continue
		}
		family, routeID, _, ok := curatedProviderIdentity(*p)
		if !ok {
			continue
		}
		keyEnv := strings.TrimSpace(p.APIKeyEnv)
		idx := -1
		for j, account := range c.ProviderAccounts {
			if account.ProviderID == family && !account.Retired && (keyEnv == "" || strings.TrimSpace(account.APIKeyEnv) == keyEnv) {
				idx = j
				break
			}
		}
		if idx < 0 {
			id := MainProviderAccountID
			if c.hasProviderFamilyAccount(family) {
				id = uniqueProviderAccountID(family, SuggestProviderAccountID(family, keyEnv), usedIDs)
			}
			account := ProviderAccount{
				ProviderID: family,
				PresetID:   strings.TrimSpace(p.PresetID),
				ID:         id,
				Label:      defaultAccountLabel(id),
				APIKeyEnv:  keyEnv,
				Default:    !c.hasProviderFamilyDefault(family),
			}
			if account.APIKeyEnv == "" {
				account.APIKeyEnv = baseAPIKeyEnvForGroup(family)
			}
			if err := validateProviderAccount(account); err != nil {
				continue
			}
			c.ProviderAccounts = append(c.ProviderAccounts, account)
			usedIDs[account.key()] = true
			idx = len(c.ProviderAccounts) - 1
		}
		if routeID == "" {
			routeID = p.Name
		}
		stampAccountMetadata(p, c.ProviderAccounts[idx], routeID)
		changed = true
	}
	return changed
}

func inferAccountPresetID(c *Config, indexes []int) string {
	if c == nil {
		return ""
	}
	for _, idx := range indexes {
		if id := strings.TrimSpace(c.Providers[idx].PresetID); id != "" {
			return id
		}
	}
	return ""
}

func legacyAccountID(keyEnv, family string, used map[providerAccountKey]bool) string {
	id := legacyAccountIDPrefix + providerIdentityHash(family + "\x00" + keyEnv)[:6]
	return uniqueProviderAccountID(family, id, used)
}
