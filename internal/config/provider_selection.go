package config

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ProviderRouteDefinition describes one protocol route in a curated family.
// Models is derived from the preset and is used only for deterministic route
// selection; secrets and mutable provider fields remain in ProviderEntry.
type ProviderRouteDefinition struct {
	ID           string
	PresetID     string
	Kind         string
	DisplayOrder int
	Models       []string
}

// ProviderFamilyDefinition is the user-facing grouping of curated presets.
type ProviderFamilyDefinition struct {
	ID                  string
	PresetIDs           []string
	RecommendedPresetID string
	Routes              []ProviderRouteDefinition
}

// ProviderSelection is the stable family/account/model identity used by new
// callers. ProviderEntry names remain a compatibility projection.
type ProviderSelection struct {
	FamilyID  string
	AccountID string
	Model     string
}

// SelectionForProviderModel projects a materialized provider entry into the
// canonical family/account/model identity. It returns false for ordinary
// custom providers, which continue using their provider/model reference.
func (c *Config) SelectionForProviderModel(entry ProviderEntry, model string) (ProviderSelection, bool) {
	model = strings.TrimSpace(model)
	if c == nil || model == "" {
		return ProviderSelection{}, false
	}
	if family, account, ok := ProviderAccountIdentity(entry); ok {
		if _, _, found := c.lookupProviderAccount(family, account); found {
			return ProviderSelection{FamilyID: family, AccountID: account, Model: model}, true
		}
	}
	if family, _, _, ok := curatedProviderIdentity(entry); ok {
		if _, _, found := c.lookupProviderAccount(family, MainProviderAccountID); found {
			return ProviderSelection{FamilyID: family, AccountID: MainProviderAccountID, Model: model}, true
		}
	}
	return ProviderSelection{}, false
}

func (s ProviderSelection) Ref() string {
	return strings.TrimSpace(s.FamilyID) + "/" + strings.TrimSpace(s.AccountID) + "/" + strings.TrimSpace(s.Model)
}

// CuratedProviderFamilies derives deterministic family metadata from the
// curated preset registry. No provider name or endpoint is used as identity.
func CuratedProviderFamilies() []ProviderFamilyDefinition {
	byID := map[string]*ProviderFamilyDefinition{}
	for _, preset := range CuratedProviderPresets() {
		familyID := preset.resolvedAccountGroupID()
		if familyID == "" {
			continue
		}
		family := byID[familyID]
		if family == nil {
			family = &ProviderFamilyDefinition{ID: familyID}
			byID[familyID] = family
		}
		family.PresetIDs = appendUniqueString(family.PresetIDs, preset.ID)
		if preset.Recommended && preferredPreset(preset.ID, family.RecommendedPresetID) {
			family.RecommendedPresetID = preset.ID
		}
		for _, entry := range preset.Entries {
			routeID := strings.TrimSpace(entry.Name)
			if routeID == "" {
				continue
			}
			idx := -1
			for i := range family.Routes {
				if family.Routes[i].ID == routeID {
					idx = i
					break
				}
			}
			if idx < 0 {
				family.Routes = append(family.Routes, ProviderRouteDefinition{
					ID: routeID, PresetID: preset.ID, Kind: strings.TrimSpace(entry.Kind),
					DisplayOrder: preset.DisplayOrder, Models: append([]string(nil), entry.ModelList()...),
				})
				continue
			}
			family.Routes[idx].Models = mergeSelectionModels(family.Routes[idx].Models, entry.ModelList())
			if preset.DisplayOrder < family.Routes[idx].DisplayOrder {
				family.Routes[idx].DisplayOrder = preset.DisplayOrder
				family.Routes[idx].PresetID = preset.ID
			}
		}
	}
	families := make([]ProviderFamilyDefinition, 0, len(byID))
	for _, family := range byID {
		// Include migrated DeepSeek default routes in the family catalog.
		for _, template := range accountRouteTemplates(family.ID) {
			found := false
			for _, route := range family.Routes {
				if route.ID == template.RouteID {
					found = true
					break
				}
			}
			if !found {
				family.Routes = append(family.Routes, ProviderRouteDefinition{
					ID: template.RouteID, PresetID: template.Entry.PresetID,
					Kind: strings.TrimSpace(template.Entry.Kind), Models: append([]string(nil), template.Entry.ModelList()...),
				})
			}
		}
		if family.RecommendedPresetID == "" {
			for _, presetID := range family.PresetIDs {
				if preferredPreset(presetID, family.RecommendedPresetID) {
					family.RecommendedPresetID = presetID
				}
			}
		}
		sort.Strings(family.PresetIDs)
		sort.SliceStable(family.Routes, func(i, j int) bool {
			if family.Routes[i].DisplayOrder != family.Routes[j].DisplayOrder {
				return family.Routes[i].DisplayOrder < family.Routes[j].DisplayOrder
			}
			return family.Routes[i].ID < family.Routes[j].ID
		})
		families = append(families, *family)
	}
	sort.Slice(families, func(i, j int) bool { return families[i].ID < families[j].ID })
	return families
}

func presetRank(p ProviderPreset) int {
	if p.Recommended {
		return -1
	}
	return p.DisplayOrder
}

func preferredPreset(candidate, current string) bool {
	if current == "" {
		return true
	}
	candidateRank, currentRank := presetRankByID(candidate), presetRankByID(current)
	return candidateRank < currentRank || candidateRank == currentRank && candidate < current
}

func presetRankByID(id string) int {
	if preset, ok := CuratedProviderPreset(id); ok {
		return presetRank(preset)
	}
	return int(^uint(0) >> 1)
}

func appendUniqueString(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func mergeSelectionModels(primary, extra []string) []string {
	seen := make(map[string]bool, len(primary)+len(extra))
	out := make([]string, 0, len(primary)+len(extra))
	for _, list := range [][]string{primary, extra} {
		for _, model := range list {
			model = strings.TrimSpace(model)
			if model == "" || seen[model] {
				continue
			}
			seen[model] = true
			out = append(out, model)
		}
	}
	return out
}

func ParseProviderSelection(c *Config, ref string) (ProviderSelection, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ProviderSelection{}, fmt.Errorf("provider selection is empty")
	}
	parts := strings.SplitN(ref, "/", 3)
	if len(parts) == 3 && IsProviderAccountID(strings.TrimSpace(parts[1])) {
		familyID, accountID, model := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
		if model != "" && isCuratedFamilyID(c, familyID) {
			return ProviderSelection{FamilyID: familyID, AccountID: accountID, Model: model}, nil
		}
	}
	provider, model, hasModel := strings.Cut(ref, "/")
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if !hasModel || provider == "" || model == "" {
		return ProviderSelection{}, fmt.Errorf("provider selection %q must include provider and model", ref)
	}
	if strings.Contains(provider, "/") {
		return ProviderSelection{}, fmt.Errorf("provider selection %q has an invalid provider", ref)
	}
	if c != nil {
		if entry, ok := c.Provider(provider); ok {
			if family, account, identityOK := ProviderAccountIdentity(*entry); identityOK {
				return ProviderSelection{FamilyID: family, AccountID: account, Model: model}, nil
			}
			if family, route, _, identityOK := curatedProviderIdentity(*entry); identityOK {
				_ = route
				return ProviderSelection{FamilyID: family, AccountID: MainProviderAccountID, Model: model}, nil
			}
		}
	}
	if family, accountID, ok := splitGeneratedProviderName(provider); ok {
		return ProviderSelection{FamilyID: family, AccountID: accountID, Model: model}, nil
	}
	if c != nil {
		if _, ok := c.DefaultAccount(provider); ok {
			return ProviderSelection{FamilyID: provider, AccountID: MainProviderAccountID, Model: model}, nil
		}
	}
	if family, route, _, ok := knownProviderIdentity(provider); ok {
		_ = route
		return ProviderSelection{FamilyID: family, AccountID: MainProviderAccountID, Model: model}, nil
	}
	return ProviderSelection{}, fmt.Errorf("provider %q is not a curated provider family", provider)
}

func isCuratedFamilyID(c *Config, familyID string) bool {
	for _, family := range CuratedProviderFamilies() {
		if family.ID == familyID {
			return true
		}
	}
	if c != nil {
		_, ok := c.DefaultAccount(familyID)
		return ok
	}
	return false
}

func splitGeneratedProviderName(provider string) (family, accountID string, ok bool) {
	base, account, found := strings.Cut(strings.TrimSpace(provider), "--")
	if !found || base == "" || account == "" || !IsProviderAccountID(account) {
		return "", "", false
	}
	if family, _, _, known := knownProviderIdentity(base); known {
		return family, account, true
	}
	for _, familyDef := range CuratedProviderFamilies() {
		for _, route := range familyDef.Routes {
			if route.ID == base {
				return familyDef.ID, account, true
			}
		}
	}
	return "", "", false
}

func (c *Config) ResolveSelection(selection ProviderSelection) (*ProviderEntry, error) {
	if c == nil {
		return nil, fmt.Errorf("resolve provider selection: nil config")
	}
	selection.FamilyID = strings.TrimSpace(selection.FamilyID)
	selection.AccountID = strings.TrimSpace(selection.AccountID)
	selection.Model = strings.TrimSpace(selection.Model)
	if selection.FamilyID == "" || selection.AccountID == "" || selection.Model == "" {
		return nil, fmt.Errorf("provider selection requires family, account, and model")
	}
	_, account, ok := c.lookupProviderAccount(selection.FamilyID, selection.AccountID)
	if !ok {
		return nil, fmt.Errorf("provider account %s/%s not found", selection.FamilyID, selection.AccountID)
	}
	if !account.IsEnabled() {
		return nil, fmt.Errorf("provider account %s/%s is unavailable", selection.FamilyID, selection.AccountID)
	}
	route, err := c.RouteForSelection(selection)
	if err != nil {
		return nil, err
	}
	for _, entry := range c.Providers {
		if entry.AccountProviderID == selection.FamilyID && entry.AccountID == selection.AccountID && entry.AccountRouteID == route.ID && entry.HasModel(selection.Model) {
			return copyResolvedEntry(&entry, selection.Model), nil
		}
	}
	return nil, fmt.Errorf("model %q is not available on %s/%s", selection.Model, selection.FamilyID, selection.AccountID)
}

func (c *Config) ResolveSelectionRef(ref string) (*ProviderEntry, ProviderSelection, error) {
	selection, err := ParseProviderSelection(c, ref)
	if err != nil {
		return nil, ProviderSelection{}, err
	}
	entry, err := c.ResolveSelection(selection)
	return entry, selection, err
}

func (c *Config) RouteForSelection(selection ProviderSelection) (ProviderRouteDefinition, error) {
	if c == nil {
		return ProviderRouteDefinition{}, fmt.Errorf("resolve provider route: nil config")
	}
	families := CuratedProviderFamilies()
	var family *ProviderFamilyDefinition
	for i := range families {
		if families[i].ID == strings.TrimSpace(selection.FamilyID) {
			family = &families[i]
			break
		}
	}
	if family == nil {
		return ProviderRouteDefinition{}, fmt.Errorf("provider family %q is not curated", selection.FamilyID)
	}
	_, account, ok := c.lookupProviderAccount(selection.FamilyID, selection.AccountID)
	if !ok {
		return ProviderRouteDefinition{}, fmt.Errorf("provider account %s/%s not found", selection.FamilyID, selection.AccountID)
	}
	disabledRoutes := make([]string, 0, len(account.DisabledRoutes))
	for _, route := range family.Routes {
		if providerAccountRouteDisabled(account, route.ID) {
			disabledRoutes = append(disabledRoutes, route.ID)
			continue
		}
		for _, entry := range c.Providers {
			if entry.AccountProviderID == account.ProviderID && entry.AccountID == account.ID && entry.AccountRouteID == route.ID && entry.HasModel(selection.Model) {
				return route, nil
			}
		}
	}
	if len(disabledRoutes) > 0 {
		return ProviderRouteDefinition{}, fmt.Errorf("model %q has no enabled route for %s/%s (disabled routes: %s)", selection.Model, selection.FamilyID, selection.AccountID, strings.Join(disabledRoutes, ", "))
	}
	return ProviderRouteDefinition{}, fmt.Errorf("model %q has no enabled route for %s/%s", selection.Model, selection.FamilyID, selection.AccountID)
}

func (c *Config) DefaultSelection(familyID string) (ProviderSelection, bool) {
	if c == nil {
		return ProviderSelection{}, false
	}
	account, ok := c.DefaultAccount(strings.TrimSpace(familyID))
	if !ok {
		return ProviderSelection{}, false
	}
	entries, ok := c.ResolveAccountProvider(account.ProviderID, account.ID)
	if !ok {
		return ProviderSelection{}, false
	}
	families := CuratedProviderFamilies()
	var family *ProviderFamilyDefinition
	for i := range families {
		if families[i].ID == account.ProviderID {
			family = &families[i]
			break
		}
	}
	if family == nil {
		return ProviderSelection{}, false
	}
	for _, route := range family.Routes {
		if providerAccountRouteDisabled(account, route.ID) {
			continue
		}
		for _, entry := range entries {
			if entry.AccountRouteID != route.ID || len(entry.ChatModelList()) == 0 || !entry.Configured() {
				continue
			}
			model := entry.DefaultModel()
			if model == "" {
				model = entry.ChatModelList()[0]
			}
			return ProviderSelection{FamilyID: account.ProviderID, AccountID: account.ID, Model: model}, true
		}
	}
	return ProviderSelection{}, false
}

func (c *Config) ResolveNewSessionSelection() (ProviderSelection, bool) {
	if c == nil {
		return ProviderSelection{}, false
	}
	if ref, _, ok := c.ResolveNewSessionChatModel(); ok {
		if selection, err := ParseProviderSelection(c, ref); err == nil {
			return selection, true
		}
		// A valid custom provider/model remains outside the curated selection
		// schema; do not silently replace it with the first curated family.
		return ProviderSelection{}, false
	}
	for _, family := range CuratedProviderFamilies() {
		if selection, ok := c.DefaultSelection(family.ID); ok {
			return selection, true
		}
	}
	return ProviderSelection{}, false
}
