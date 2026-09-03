package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
)

// runProviderCommand handles "/provider": with no argument it opens the provider
// picker; "/provider <name>" switches to that
// provider's default model (or prompts the user to pick one when multiple models
// are configured).
func (m *chatTUI) runProviderCommand(input string) {
	args := tokenizeArgs(input) // args[0] == "/provider"
	if len(args) < 2 {
		m.openProviderPicker()
		return
	}
	name := args[1]
	m.switchToProvider(name)
}

func (m *chatTUI) openProviderPicker() {
	cfg, err := config.Load()
	if err != nil {
		m.notice("provider: " + err.Error())
		return
	}
	curProvider := strings.SplitN(m.modelRef, "/", 2)[0]
	curSelection, _ := config.ParseProviderSelection(cfg, m.modelRef)
	var items []quickPickerItem
	selected := 0
	seen := map[string]bool{}
	for _, account := range cfg.ProviderAccounts {
		if !account.IsEnabled() {
			continue
		}
		entries, ok := cfg.ResolveAccountProvider(account.ProviderID, account.ID)
		if !ok {
			continue
		}
		entries = selectableAccountEntries(account, entries)
		if len(entries) == 0 {
			continue
		}
		usable := false
		for i := range entries {
			if entries[i].Configured() {
				usable = true
			}
			seen[entries[i].Name] = true
		}
		if !usable {
			continue
		}
		status := ""
		for i := range entries {
			if entries[i].Name == curProvider || (curSelection.FamilyID == account.ProviderID && curSelection.AccountID == account.ID) {
				status = "active"
				selected = len(items)
			}
		}
		modelNames := make([]string, 0, len(entries))
		for _, entry := range entries {
			models := entry.ChatModelList()
			if len(models) == 0 {
				models = entry.ModelList()
			}
			modelNames = append(modelNames, models...)
		}
		items = append(items, quickPickerItem{
			ID: account.ProviderID + "/" + account.ID, Label: account.Label,
			Description: fmt.Sprintf("%s · %d route(s) · %s", account.ProviderID, len(entries), strings.Join(modelNames, ", ")), Status: status,
		})
	}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if seen[p.Name] || !p.Configured() {
			continue
		}
		models := p.ChatModelList()
		if len(models) == 0 {
			models = p.ModelList()
		}
		status := ""
		if p.Name == curProvider {
			status = "active"
			selected = len(items)
		}
		items = append(items, quickPickerItem{
			ID: p.Name, Label: p.Name,
			Description: fmt.Sprintf("%s · %d model(s)", p.Kind, len(models)), Status: status,
		})
	}
	if len(items) == 0 {
		m.notice("provider: no configured providers")
		return
	}
	m.quickPick = &quickPicker{kind: quickPickerProvider, title: "Select provider", items: items, selected: selected}
}

// switchToProvider switches the session to the named provider's default model.
// If the provider has multiple models, it shows an interactive picker (in the
// setup/CLI style) if running in a TTY, or falls back to a notice listing
// available models.
func (m *chatTUI) switchToProvider(name string) {
	cfg, err := config.Load()
	if err != nil {
		m.notice("provider: " + err.Error())
		return
	}
	var entry *config.ProviderEntry
	entry = providerEntryForAccountSelection(cfg, name)
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if p.Name == name && p.Configured() && providerEntrySelectable(cfg, *p) {
			entry = p
			break
		}
	}
	if entry == nil {
		if account, ok := cfg.DefaultAccount(name); ok {
			for _, candidate := range selectableAccountEntries(account, mustResolveAccountProvider(cfg, account.ProviderID, account.ID)) {
				if candidate.Configured() && len(candidate.ChatModelList()) > 0 {
					copy := candidate
					entry = &copy
					break
				}
			}
		}
	}
	if entry == nil {
		m.notice(fmt.Sprintf(i18n.M.ProviderUnknownFmt, name))
		return
	}

	// Determine current provider.
	curProvider := ""
	if parts := strings.SplitN(m.modelRef, "/", 2); len(parts) == 2 {
		curProvider = parts[0]
	}

	models := entry.ChatModelList()
	if len(models) == 0 {
		models = entry.ModelList()
	}
	if len(models) == 0 {
		m.notice(fmt.Sprintf(i18n.M.ProviderNoModelsFmt, name))
		return
	}

	// If only one model, switch directly.
	if len(models) == 1 {
		ref := entry.Name + "/" + models[0]
		if selection, ok := cfg.SelectionForProviderModel(*entry, models[0]); ok {
			ref = selection.Ref()
		}
		if entry.Name == curProvider && models[0] == "" {
			m.notice(fmt.Sprintf(i18n.M.ProviderAlreadyOnFmt, name))
			return
		}
		m.runModelSubcommand("/model " + ref)
		return
	}

	items := make([]quickPickerItem, 0, len(models))
	selected := 0
	currentSelection, _ := config.ParseProviderSelection(cfg, m.modelRef)
	for _, model := range models {
		ref := entry.Name + "/" + model
		if selection, ok := cfg.SelectionForProviderModel(*entry, model); ok {
			ref = selection.Ref()
		}
		status := ""
		if ref == m.modelRef || (currentSelection.Model == model && currentSelection.FamilyID == entry.AccountProviderID && currentSelection.AccountID == entry.AccountID) {
			status = "active"
			selected = len(items)
		}
		items = append(items, quickPickerItem{ID: ref, Label: model, Description: entry.Name, Status: status})
	}
	m.quickPick = &quickPicker{
		kind: quickPickerProviderModel, title: fmt.Sprintf(i18n.M.ProviderPickLabel, name),
		items: items, selected: selected,
	}
}

func mustResolveAccountProvider(cfg *config.Config, providerID, accountID string) []config.ProviderEntry {
	entries, _ := cfg.ResolveAccountProvider(providerID, accountID)
	return entries
}

func providerEntryForAccountSelection(cfg *config.Config, name string) *config.ProviderEntry {
	family, accountID, ok := strings.Cut(strings.TrimSpace(name), "/")
	if !ok || !config.IsProviderAccountID(accountID) {
		return nil
	}
	for _, account := range cfg.ProviderAccounts {
		if account.ProviderID != family || account.ID != accountID || !account.IsEnabled() {
			continue
		}
		for _, candidate := range selectableAccountEntries(account, mustResolveAccountProvider(cfg, family, accountID)) {
			if candidate.Configured() && len(candidate.ChatModelList()) > 0 {
				copy := candidate
				return &copy
			}
		}
	}
	return nil
}

func selectableAccountEntries(account config.ProviderAccount, entries []config.ProviderEntry) []config.ProviderEntry {
	if len(entries) == 0 {
		return nil
	}
	disabled := make(map[string]bool, len(account.DisabledRoutes))
	for _, route := range account.DisabledRoutes {
		disabled[strings.TrimSpace(route)] = true
	}
	out := make([]config.ProviderEntry, 0, len(entries))
	for _, entry := range entries {
		if disabled[strings.TrimSpace(entry.AccountRouteID)] {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func providerEntrySelectable(cfg *config.Config, entry config.ProviderEntry) bool {
	account, ok := config.ProviderAccountForEntry(cfg, entry)
	return !ok || account.IsEnabled()
}

func (m chatTUI) handleQuickPickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.quickPick
	if p == nil {
		return m, nil
	}
	result := p.handleKey(msg)
	if result.cancelled {
		m.quickPick = nil
		return m, nil
	}
	if result.choice == nil {
		return m, nil
	}
	kind := p.kind
	choice := *result.choice
	m.quickPick = nil
	switch kind {
	case quickPickerModel, quickPickerProviderModel:
		m.runModelSubcommand("/model " + choice.ID)
		if m.pendingModelSwitch != nil {
			return m, m.pendingModelSwitch
		}
	case quickPickerProvider:
		m.switchToProvider(choice.ID)
		if m.pendingModelSwitch != nil {
			return m, m.pendingModelSwitch
		}
	}
	return m, nil
}

func (m chatTUI) renderQuickPicker() string {
	if m.quickPick == nil {
		return ""
	}
	return m.quickPick.render(m.width)
}
