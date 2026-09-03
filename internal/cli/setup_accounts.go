package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
)

type setupMenuKind uint8

const (
	setupMenuProvider setupMenuKind = iota
	setupMenuAccount
	setupMenuAddAccount
	setupMenuAddOpenAI
	setupMenuAddAnthropic
	setupMenuSave
	setupMenuCancel
)

type setupMenuAction struct {
	kind       setupMenuKind
	provider   int
	providerID string
	accountID  string
}

func providerManagerMenu(s *providerSetupSession) ([]menuItem, []setupMenuAction) {
	items := make([]menuItem, 0, len(s.cfg.Providers)+8)
	actions := make([]setupMenuAction, 0, len(s.cfg.Providers)+8)
	seen := map[string]bool{}
	for _, account := range s.cfg.ProviderAccounts {
		entries, _ := s.cfg.ResolveAccountProvider(account.ProviderID, account.ID)
		keyStatus := i18n.M.SetupKeyMissing
		if account.APIKeyEnv == "" || config.CredentialIsSet(account.APIKeyEnv) || s.pendingCredentials[account.APIKeyEnv] != "" {
			keyStatus = i18n.M.SetupKeySet
		}
		desc := fmt.Sprintf("%s · %d · %s", account.ProviderID, len(entries), keyStatus)
		if account.Default {
			desc += " · " + i18n.M.SetupDefaultBadge
		}
		if !account.IsEnabled() {
			desc += " · disabled"
		}
		if account.Retired {
			desc += " · retired"
		}
		items = append(items, menuItem{name: account.Label, desc: desc})
		actions = append(actions, setupMenuAction{kind: setupMenuAccount, providerID: account.ProviderID, accountID: account.ID})
		for _, e := range entries {
			seen[e.Name] = true
		}
	}
	for i, p := range s.cfg.Providers {
		if seen[p.Name] {
			continue
		}
		models := p.ModelList()
		keyStatus := i18n.M.SetupKeyMissing
		if p.APIKeyEnv == "" || config.CredentialIsSet(p.APIKeyEnv) || s.pendingCredentials[p.APIKeyEnv] != "" {
			keyStatus = i18n.M.SetupKeySet
		}
		desc := fmt.Sprintf("%s · %d %s · %s", p.Kind, len(models), i18n.M.SetupModelsUnit, keyStatus)
		if s.cfg.DefaultModel == p.Name || config.ModelRefsProvider(s.cfg.DefaultModel, p.Name) {
			desc += " · " + i18n.M.SetupDefaultBadge
		}
		items = append(items, menuItem{name: p.Name, desc: desc})
		actions = append(actions, setupMenuAction{kind: setupMenuProvider, provider: i})
	}
	if !s.projectScoped {
		items = append(items, menuItem{name: i18n.M.SetupAddAccount, desc: i18n.M.SetupAddAccountDesc})
		actions = append(actions, setupMenuAction{kind: setupMenuAddAccount})
	}
	items = append(items,
		menuItem{name: i18n.M.SetupAddOpenAI, desc: i18n.M.CustomProviderDesc},
		menuItem{name: i18n.M.SetupAddAnthropic, desc: i18n.M.AnthropicProviderDesc},
		menuItem{name: i18n.M.SetupSaveExit, desc: i18n.M.SetupSaveExitDesc},
		menuItem{name: i18n.M.SetupCancel, desc: i18n.M.SetupCancelDesc},
	)
	actions = append(actions,
		setupMenuAction{kind: setupMenuAddOpenAI},
		setupMenuAction{kind: setupMenuAddAnthropic},
		setupMenuAction{kind: setupMenuSave},
		setupMenuAction{kind: setupMenuCancel},
	)
	return items, actions
}

func manageProviderAccount(s *providerSetupSession, providerID, accountID string) {
	_, account, ok := lookupSessionAccount(s, providerID, accountID)
	if !ok {
		return
	}
	items := []menuItem{{name: i18n.M.SetupUpdateKey}}
	var routeIDs []string
	if !account.Retired {
		items = append(items,
			menuItem{name: i18n.M.SetupSetDefault},
			menuItem{name: i18n.M.SetupAccountRename},
			menuItem{name: i18n.M.SetupAccountToggle},
			menuItem{name: i18n.M.SetupAccountRetire},
		)
		seenRoutes := map[string]bool{}
		for _, entry := range s.cfg.Providers {
			if entry.AccountProviderID != account.ProviderID || entry.AccountID != account.ID {
				continue
			}
			routeID := strings.TrimSpace(entry.AccountRouteID)
			if routeID == "" || seenRoutes[routeID] {
				continue
			}
			seenRoutes[routeID] = true
			routeIDs = append(routeIDs, routeID)
		}
		for _, routeID := range account.DisabledRoutes {
			routeID = strings.TrimSpace(routeID)
			if routeID != "" && !seenRoutes[routeID] {
				seenRoutes[routeID] = true
				routeIDs = append(routeIDs, routeID)
			}
		}
		sort.Strings(routeIDs)
		for _, routeID := range routeIDs {
			disabled := containsString(account.DisabledRoutes, routeID)
			action := "Enable route"
			if !disabled {
				action = "Disable route"
			}
			items = append(items, menuItem{name: fmt.Sprintf("%s: %s", action, routeID)})
		}
	} else {
		items = append(items, menuItem{name: "Restore account"})
	}
	items = append(items, menuItem{name: i18n.M.SetupBack})
	idx, err := selectOne(fmt.Sprintf("%s / %s", account.ProviderID, account.Label), items)
	if err != nil || idx == len(items)-1 {
		return
	}
	switch idx {
	case 0:
		updateAccountKey(s, account)
	case 1:
		if account.Retired {
			if err := s.restoreProviderAccount(account.ProviderID, account.ID); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			return
		}
		if err := s.mutateProviderAccount(account.ProviderID, account.ID, func() error {
			return s.cfg.SetProviderAccountDefault(account.ProviderID, account.ID)
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	case 2:
		renameSessionAccount(s, account)
	case 3:
		if err := s.mutateProviderAccount(account.ProviderID, account.ID, func() error {
			return s.cfg.SetProviderAccountEnabled(account.ProviderID, account.ID, !account.IsEnabled())
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	case 4:
		if err := s.mutateProviderAccount(account.ProviderID, account.ID, func() error {
			return s.cfg.RetireProviderAccount(account.ProviderID, account.ID)
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
		} else {
			entries, _ := s.cfg.ResolveAccountProvider(account.ProviderID, account.ID)
			for _, entry := range entries {
				s.removeProviderAccess(entry.Name)
			}
		}
	default:
		// Account actions occupy slots 0..4. Route toggles follow them.
		if account.Retired {
			return
		}
		if routeIndex := idx - 5; routeIndex >= 0 && routeIndex < len(routeIDs) {
			routeID := routeIDs[routeIndex]
			disabled := containsString(account.DisabledRoutes, routeID)
			if err := s.setProviderAccountRouteEnabled(account.ProviderID, account.ID, routeID, disabled); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
	}
}

func addProviderAccountToSession(s *providerSetupSession) bool {
	if s.projectScoped {
		fmt.Fprintln(os.Stderr, i18n.M.SetupProjectNoAccounts)
		return false
	}
	presets := curatedAccountSetupPresets()
	if len(presets) == 0 {
		return false
	}
	items := make([]menuItem, 0, len(presets))
	for _, preset := range presets {
		items = append(items, menuItem{name: preset.Label, desc: preset.AccountGroupID})
	}
	idx, err := selectOne(i18n.M.SetupAddAccount, items)
	if err != nil || idx < 0 || idx >= len(presets) {
		return false
	}
	preset := presets[idx]
	label := strings.TrimSpace(askLine(i18n.M.SetupAccountLabel, "Team"))
	if label == "" {
		return false
	}
	key := strings.TrimSpace(askCredentialLine())
	account, err := s.cfg.AddProviderAccount(preset.AccountGroupID, preset.ID, label, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return false
	}
	s.recordProviderAccountMutation(account.ProviderID, account.ID, nil, providerSetupAccountPtr(account), "", nil, nil)
	if key != "" {
		if err := s.setCredential(account.APIKeyEnv, key); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return false
		}
	}
	entries, _ := s.cfg.ResolveAccountProvider(account.ProviderID, account.ID)
	s.addProviderAccess(entries)
	s.promoteDefaultToNewProviders(entries)
	return true
}

// curatedAccountSetupPresets returns one deterministic preset per curated
// provider account group. Recommended presets win; ties use display order and
// then ID so adding a new curated route cannot make setup nondeterministic.
func curatedAccountSetupPresets() []config.ProviderPreset {
	byGroup := map[string]config.ProviderPreset{}
	for _, preset := range config.CuratedProviderPresets() {
		group := strings.TrimSpace(preset.AccountGroupID)
		if group == "" {
			continue
		}
		current, ok := byGroup[group]
		if !ok || accountSetupPresetLess(preset, current) {
			byGroup[group] = preset
		}
	}
	out := make([]config.ProviderPreset, 0, len(byGroup))
	for _, preset := range byGroup {
		out = append(out, preset)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].AccountGroupID < out[j].AccountGroupID
	})
	return out
}

func accountSetupPresetLess(a, b config.ProviderPreset) bool {
	if a.Recommended != b.Recommended {
		return a.Recommended
	}
	if a.DisplayOrder != b.DisplayOrder {
		return a.DisplayOrder < b.DisplayOrder
	}
	return a.ID < b.ID
}

func updateAccountKey(s *providerSetupSession, account config.ProviderAccount) {
	if strings.TrimSpace(account.APIKeyEnv) == "" {
		return
	}
	value := strings.TrimSpace(askCredentialLine())
	if value == "" {
		return
	}
	if err := s.setCredential(account.APIKeyEnv, value); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func renameSessionAccount(s *providerSetupSession, account config.ProviderAccount) {
	label := strings.TrimSpace(askLine(i18n.M.SetupAccountLabel, account.Label))
	if label == "" {
		return
	}
	if err := s.mutateProviderAccount(account.ProviderID, account.ID, func() error {
		return s.cfg.RenameProviderAccount(account.ProviderID, account.ID, label)
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func lookupSessionAccount(s *providerSetupSession, providerID, accountID string) (int, config.ProviderAccount, bool) {
	for i, account := range s.cfg.ProviderAccounts {
		if account.ProviderID == providerID && account.ID == accountID {
			return i, account, true
		}
	}
	return -1, config.ProviderAccount{}, false
}

func askLine(label, def string) string {
	// Defaults are kept out of the rendered prompt so a future caller cannot
	// accidentally send a credential value to a logging/output sink.
	fmt.Printf("%s: ", label)
	var line string
	_, _ = fmt.Scanln(&line)
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func askCredentialLine() string {
	// Keep the credential prompt constant so tainted input cannot reach logging.
	fmt.Print("API key: ")
	var line string
	_, _ = fmt.Scanln(&line)
	return strings.TrimSpace(line)
}
