package config

import "strings"

func (c *Config) DefaultAccount(providerID string) (ProviderAccount, bool) {
	if c == nil {
		return ProviderAccount{}, false
	}
	providerID = strings.TrimSpace(providerID)
	var first ProviderAccount
	found := false
	for _, account := range c.ProviderAccounts {
		if account.ProviderID != providerID || !account.IsEnabled() {
			continue
		}
		if !found {
			first = account
			found = true
		}
		if account.Default {
			return cloneProviderAccount(account), true
		}
	}
	if !found {
		return ProviderAccount{}, false
	}
	return cloneProviderAccount(first), true
}

func (c *Config) ResolveAccountProvider(providerID, accountID string) ([]ProviderEntry, bool) {
	if c == nil {
		return nil, false
	}
	providerID = strings.TrimSpace(providerID)
	accountID = strings.TrimSpace(accountID)
	if providerID == "" || accountID == "" {
		return nil, false
	}
	out := make([]ProviderEntry, 0, 2)
	for i := range c.Providers {
		p := c.Providers[i]
		if p.AccountProviderID == providerID && p.AccountID == accountID {
			out = append(out, p)
		}
	}
	return out, len(out) > 0
}

func (c *Config) AccountEnabled(providerID, accountID string) bool {
	_, account, ok := c.lookupProviderAccount(providerID, accountID)
	if !ok {
		return true
	}
	return account.IsEnabled()
}

func (c *Config) accountSelectable(e ProviderEntry) bool {
	providerID, accountID, ok := ProviderAccountIdentity(e)
	if !ok {
		return true
	}
	return c.AccountEnabled(providerID, accountID)
}

func (c *Config) Provider(name string) (*ProviderEntry, bool) {
	if c == nil {
		return nil, false
	}
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			return &c.Providers[i], true
		}
	}
	return nil, false
}

func (c *Config) ResolveModel(ref string) (*ProviderEntry, bool) {
	if c == nil || ref == "" {
		return nil, false
	}
	// New curated references use family/account/model. Resolve them through the
	// account selection layer before consulting compatibility provider names.
	if strings.Count(strings.TrimSpace(ref), "/") >= 2 {
		if selection, err := ParseProviderSelection(c, ref); err == nil {
			if entry, resolveErr := c.ResolveSelection(selection); resolveErr == nil {
				return entry, true
			}
		}
	}
	if access := desktopProviderAccessMap(c.Desktop.ProviderAccess); len(access) > 0 {
		if access["deepseek"] && !canCanonicalizeLegacyDeepSeekProviders(c) {
			delete(access, "deepseek")
		}
		ref = retargetDesktopOfficialRef(ref, access)
	}
	if prov, model, ok := strings.Cut(ref, "/"); ok {
		if e, found := c.Provider(prov); found && e.HasModel(model) {
			return copyResolvedEntry(e, model), true
		}
		if e, found := c.resolveFamilyModel(prov, model); found {
			return e, true
		}
	}
	if e, found := c.Provider(ref); found {
		return copyResolvedEntry(e, e.DefaultModel()), true
	}
	if e, found := c.resolveFamilyModel(ref, ""); found {
		return e, true
	}
	for i := range c.Providers {
		if c.Providers[i].HasModel(ref) {
			return copyResolvedEntry(&c.Providers[i], ref), true
		}
	}
	return nil, false
}

func (c *Config) resolveFamilyModel(family, model string) (*ProviderEntry, bool) {
	family = strings.TrimSpace(family)
	if family == "" {
		return nil, false
	}
	if _, ok := c.Provider(family); ok {
		return nil, false
	}
	account, ok := c.DefaultAccount(family)
	if !ok {
		return nil, false
	}
	entries, ok := c.ResolveAccountProvider(account.ProviderID, account.ID)
	if !ok {
		return nil, false
	}
	model = strings.TrimSpace(model)
	if model != "" {
		for i := range entries {
			if entries[i].HasModel(model) {
				return copyResolvedEntry(&entries[i], model), true
			}
		}
		return nil, false
	}
	for i := range entries {
		if entries[i].Configured() {
			return copyResolvedEntry(&entries[i], entries[i].DefaultModel()), true
		}
	}
	e := entries[0]
	return copyResolvedEntry(&e, e.DefaultModel()), true
}

func copyResolvedEntry(e *ProviderEntry, model string) *ProviderEntry {
	cp := *e
	cp.Model = model
	cp.applyModelPrice()
	cp.applyModelOverride()
	return &cp
}

func (c *Config) ResolveModelWithFallback(ref string) (resolvedRef string, fallback bool, ok bool) {
	ref = strings.TrimSpace(ref)
	if ref != "" {
		if e, found := c.ResolveModel(ref); found {
			return e.Name + "/" + e.Model, false, true
		}
	}
	if ref != c.DefaultModel && c.DefaultModel != "" {
		if e, found := c.ResolveModel(c.DefaultModel); found && e.Configured() {
			return e.Name + "/" + e.Model, true, true
		}
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if len(p.ModelList()) == 0 || !p.Configured() || !c.accountSelectable(*p) {
			continue
		}
		return p.Name + "/" + p.DefaultModel(), true, true
	}
	return "", false, false
}

func (c *Config) ResolveNewSessionChatModel() (resolvedRef string, fallback bool, ok bool) {
	return c.resolveNewSessionChatModel(nil, true)
}

func (c *Config) resolveNewSessionChatModel(providerAllowed func(string) bool, preserveUnknownDefault bool) (resolvedRef string, fallback bool, ok bool) {
	if c == nil {
		return "", false, false
	}
	if providerAllowed == nil {
		providerAllowed = func(string) bool { return true }
	}

	def := strings.TrimSpace(c.DefaultModel)
	keylessDefault := ""
	if def != "" {
		if entry, found := c.ResolveModel(def); found {
			if providerAllowed(entry.Name) && c.accountSelectable(*entry) && IsLikelyChatModel(entry.Model) {
				if entry.Configured() {
					return def, false, true
				}
				keylessDefault = def
			}
		} else if preserveUnknownDefault {
			return def, false, true
		}
	}

	keylessFallback := ""
	for i := range c.Providers {
		p := &c.Providers[i]
		if !providerAllowed(p.Name) || !c.accountSelectable(*p) {
			continue
		}
		chatModels := p.ChatModelList()
		if len(chatModels) == 0 {
			continue
		}
		model := chatModels[0]
		for _, candidate := range chatModels {
			if candidate == p.DefaultModel() {
				model = candidate
				break
			}
		}
		resolved := p.Name + "/" + model
		if p.Configured() {
			return resolved, true, true
		}
		if keylessFallback == "" {
			keylessFallback = resolved
		}
	}
	if keylessDefault != "" {
		return keylessDefault, false, true
	}
	if keylessFallback != "" {
		return keylessFallback, true, true
	}
	return "", false, false
}

func (c *Config) ResolveDesktopNewSessionModel() (resolvedRef string, fallback bool, ok bool) {
	if c == nil {
		return "", false, false
	}
	access := desktopProviderAccessMap(c.Desktop.ProviderAccess)
	return c.resolveNewSessionChatModel(func(name string) bool {
		return c.Desktop.ProviderAccess == nil || access[strings.TrimSpace(name)]
	}, false)
}
