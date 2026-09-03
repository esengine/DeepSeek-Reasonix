package cli

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
)

func providerSetupAccountKey(providerID, accountID string) string {
	return strings.TrimSpace(providerID) + "\x00" + strings.TrimSpace(accountID)
}

func cloneProviderSetupAccount(account config.ProviderAccount) config.ProviderAccount {
	if account.Enabled != nil {
		enabled := *account.Enabled
		account.Enabled = &enabled
	}
	account.DisabledRoutes = append([]string(nil), account.DisabledRoutes...)
	return account
}

func providerSetupAccountPtr(account config.ProviderAccount) *config.ProviderAccount {
	copy := cloneProviderSetupAccount(account)
	return &copy
}

func (s *providerSetupSession) recordProviderAccountMutation(providerID, accountID string, before, after *config.ProviderAccount, routeID string, beforeRoute, afterRoute *bool) {
	var beforeAccounts, afterAccounts []config.ProviderAccount
	if before != nil {
		beforeAccounts = []config.ProviderAccount{cloneProviderSetupAccount(*before)}
	}
	if after != nil {
		afterAccounts = []config.ProviderAccount{cloneProviderSetupAccount(*after)}
	}
	s.operations = append(s.operations, providerSetupOperation{
		kind:           setupOpProviderAccount,
		providerID:     strings.TrimSpace(providerID),
		accountID:      strings.TrimSpace(accountID),
		beforeAccount:  before,
		afterAccount:   after,
		routeID:        strings.TrimSpace(routeID),
		beforeRoute:    beforeRoute,
		afterRoute:     afterRoute,
		beforeAccounts: beforeAccounts,
		afterAccounts:  afterAccounts,
	})
}

func (s *providerSetupSession) recordProviderAccountFamilyMutation(providerID, accountID string, before, after []config.ProviderAccount, routeID string) {
	var targetBefore, targetAfter *config.ProviderAccount
	for i := range before {
		if before[i].ProviderID == providerID && before[i].ID == accountID {
			value := cloneProviderSetupAccount(before[i])
			targetBefore = &value
		}
	}
	for i := range after {
		if after[i].ProviderID == providerID && after[i].ID == accountID {
			value := cloneProviderSetupAccount(after[i])
			targetAfter = &value
		}
	}
	s.recordProviderAccountMutation(providerID, accountID, targetBefore, targetAfter, routeID, nil, nil)
	if len(s.operations) == 0 {
		return
	}
	op := &s.operations[len(s.operations)-1]
	op.beforeAccounts = cloneProviderSetupAccounts(before)
	op.afterAccounts = cloneProviderSetupAccounts(after)
}

func cloneProviderSetupAccounts(accounts []config.ProviderAccount) []config.ProviderAccount {
	if accounts == nil {
		return nil
	}
	out := make([]config.ProviderAccount, len(accounts))
	for i, account := range accounts {
		out[i] = cloneProviderSetupAccount(account)
	}
	return out
}

func (s *providerSetupSession) accountFamilySnapshots(providerID string) []config.ProviderAccount {
	if s == nil || s.cfg == nil {
		return nil
	}
	var out []config.ProviderAccount
	for _, account := range s.cfg.ProviderAccounts {
		if account.ProviderID == strings.TrimSpace(providerID) {
			out = append(out, cloneProviderSetupAccount(account))
		}
	}
	return out
}

func (s *providerSetupSession) mutateProviderAccount(providerID, accountID string, mutate func() error) error {
	familyBefore := s.accountFamilySnapshots(providerID)
	defaultBefore := s.cfg.DefaultModel
	before, ok := s.snapshotAccount(providerID, accountID)
	if !ok {
		return fmt.Errorf("provider account %s/%s not found", providerID, accountID)
	}
	if err := mutate(); err != nil {
		return err
	}
	after, ok := s.snapshotAccount(providerID, accountID)
	if !ok {
		return fmt.Errorf("provider account %s/%s disappeared after mutation", providerID, accountID)
	}
	if !reflect.DeepEqual(before, after) || defaultBefore != s.cfg.DefaultModel {
		s.recordProviderAccountFamilyMutation(providerID, accountID, familyBefore, s.accountFamilySnapshots(providerID), "")
		if len(s.operations) > 0 {
			op := &s.operations[len(s.operations)-1]
			op.accountDefaultModelChanged = defaultBefore != s.cfg.DefaultModel
			op.beforeString = defaultBefore
			op.afterString = s.cfg.DefaultModel
		}
	}
	return nil
}

func (s *providerSetupSession) setProviderAccountRouteEnabled(providerID, accountID, routeID string, enabled bool) error {
	familyBefore := s.accountFamilySnapshots(providerID)
	defaultBefore := s.cfg.DefaultModel
	accessBefore := append([]string(nil), s.cfg.Desktop.ProviderAccess...)
	before, ok := s.snapshotAccount(providerID, accountID)
	if !ok {
		return fmt.Errorf("provider account %s/%s not found", providerID, accountID)
	}
	if err := s.cfg.SetProviderAccountRouteEnabled(providerID, accountID, routeID, enabled); err != nil {
		return err
	}
	after, _ := s.snapshotAccount(providerID, accountID)
	if !reflect.DeepEqual(before, after) || defaultBefore != s.cfg.DefaultModel {
		beforeEnabled := accountRouteEnabled(*before, routeID)
		afterEnabled := accountRouteEnabled(*after, routeID)
		s.recordProviderAccountFamilyMutation(providerID, accountID, familyBefore, s.accountFamilySnapshots(providerID), routeID)
		if len(s.operations) > 0 {
			op := &s.operations[len(s.operations)-1]
			op.beforeRoute = &beforeEnabled
			op.afterRoute = &afterEnabled
			op.accountDefaultModelChanged = defaultBefore != s.cfg.DefaultModel
			op.beforeString = defaultBefore
			op.afterString = s.cfg.DefaultModel
		}
	}
	s.recordAccessTransition(accessBefore)
	return nil
}

func (s *providerSetupSession) restoreProviderAccount(providerID, accountID string) error {
	familyBefore := s.accountFamilySnapshots(providerID)
	defaultBefore := s.cfg.DefaultModel
	accessBefore := append([]string(nil), s.cfg.Desktop.ProviderAccess...)
	before, ok := s.snapshotAccount(providerID, accountID)
	if !ok {
		return fmt.Errorf("provider account %s/%s not found", providerID, accountID)
	}
	if err := s.cfg.RestoreProviderAccount(providerID, accountID); err != nil {
		return err
	}
	after, _ := s.snapshotAccount(providerID, accountID)
	if !reflect.DeepEqual(before, after) || defaultBefore != s.cfg.DefaultModel {
		s.recordProviderAccountFamilyMutation(providerID, accountID, familyBefore, s.accountFamilySnapshots(providerID), "")
		if len(s.operations) > 0 {
			op := &s.operations[len(s.operations)-1]
			op.accountDefaultModelChanged = defaultBefore != s.cfg.DefaultModel
			op.beforeString = defaultBefore
			op.afterString = s.cfg.DefaultModel
		}
	}
	s.recordAccessTransition(accessBefore)
	return nil
}

func (s *providerSetupSession) snapshotAccount(providerID, accountID string) (*config.ProviderAccount, bool) {
	if s == nil || s.cfg == nil {
		return nil, false
	}
	_, account, ok := s.cfgAccount(providerID, accountID)
	if !ok {
		return nil, false
	}
	return providerSetupAccountPtr(account), true
}

func (s *providerSetupSession) cfgAccount(providerID, accountID string) (int, config.ProviderAccount, bool) {
	if s == nil || s.cfg == nil {
		return -1, config.ProviderAccount{}, false
	}
	for i, account := range s.cfg.ProviderAccounts {
		if strings.TrimSpace(account.ProviderID) == strings.TrimSpace(providerID) && strings.TrimSpace(account.ID) == strings.TrimSpace(accountID) {
			return i, account, true
		}
	}
	return -1, config.ProviderAccount{}, false
}

func (s *providerSetupSession) summary() []string {
	var added, edited []string
	for _, p := range s.cfg.Providers {
		old, existed := s.originalProviders[p.Name]
		switch {
		case !existed:
			added = append(added, p.Name)
		case !providerSetupEqual(old, p):
			edited = append(edited, p.Name)
		}
	}
	var out []string
	if len(added) > 0 {
		out = append(out, fmt.Sprintf(i18n.M.SetupSummaryAddedFmt, strings.Join(added, ", ")))
	}
	if len(edited) > 0 {
		out = append(out, fmt.Sprintf(i18n.M.SetupSummaryEditedFmt, strings.Join(edited, ", ")))
	}
	var addedAccounts, editedAccounts []string
	for _, account := range s.cfg.ProviderAccounts {
		key := providerSetupAccountKey(account.ProviderID, account.ID)
		old, existed := s.originalAccounts[key]
		label := providerSetupAccountSummary(account)
		if !existed {
			addedAccounts = append(addedAccounts, label)
		} else if !reflect.DeepEqual(old, account) {
			editedAccounts = append(editedAccounts, label)
		}
	}
	sort.Strings(addedAccounts)
	sort.Strings(editedAccounts)
	if len(addedAccounts) > 0 {
		out = append(out, fmt.Sprintf("accounts added: %s", strings.Join(addedAccounts, ", ")))
	}
	if len(editedAccounts) > 0 {
		out = append(out, fmt.Sprintf("accounts changed: %s", strings.Join(editedAccounts, ", ")))
	}
	if len(s.removed) > 0 {
		names := make([]string, 0, len(s.removed))
		for name := range s.removed {
			names = append(names, name)
		}
		sort.Strings(names)
		out = append(out, fmt.Sprintf(i18n.M.SetupSummaryRemovedFmt, strings.Join(names, ", ")))
	}
	if s.cfg.DefaultModel != s.originalDefault {
		out = append(out, fmt.Sprintf(i18n.M.SetupSummaryDefaultFmt, s.cfg.DefaultModel))
	}
	if len(s.pendingCredentials) > 0 {
		out = append(out, fmt.Sprintf(i18n.M.SetupSummaryKeysFmt, len(s.pendingCredentials)))
	}
	if len(out) == 0 {
		out = append(out, i18n.M.SetupSummaryNoChanges)
	}
	return out
}

func providerSetupAccountSummary(account config.ProviderAccount) string {
	label := fmt.Sprintf("%s/%s", account.ProviderID, account.Label)
	var states []string
	if account.Default {
		states = append(states, "default")
	}
	if account.Retired {
		states = append(states, "retired")
	} else if !account.IsEnabled() {
		states = append(states, "disabled")
	}
	if len(account.DisabledRoutes) > 0 {
		states = append(states, fmt.Sprintf("routes disabled: %s", strings.Join(account.DisabledRoutes, ", ")))
	}
	if len(states) > 0 {
		label += " (" + strings.Join(states, "; ") + ")"
	}
	return label
}

func replayProviderAccountOperation(cfg *config.Config, operation providerSetupOperation) error {
	field := fmt.Sprintf("provider account %s/%s", operation.providerID, operation.accountID)
	if strings.TrimSpace(operation.providerID) == "" || strings.TrimSpace(operation.accountID) == "" {
		return fmt.Errorf("replay provider account: missing identity")
	}
	if err := replayProviderAccountSnapshots(cfg, operation, field); err != nil {
		return err
	}
	if err := replayProviderAccountDefaultModel(cfg, operation); err != nil {
		return err
	}
	return replayProviderAccountRoute(cfg, operation, field)
}

func replayProviderAccountSnapshots(cfg *config.Config, operation providerSetupOperation, field string) error {
	before := operation.beforeAccounts
	after := operation.afterAccounts
	// Older operation snapshots contain only the target account. Keep those
	// snapshots valid while preferring full-family snapshots for mutations that
	// also change which account is default.
	if before == nil && operation.beforeAccount != nil {
		before = []config.ProviderAccount{*operation.beforeAccount}
	}
	if after == nil && operation.afterAccount != nil {
		after = []config.ProviderAccount{*operation.afterAccount}
	}
	if len(before) == 0 && len(after) == 0 {
		return fmt.Errorf("replay %s: empty account change", field)
	}
	current := make(map[string]config.ProviderAccount)
	for _, account := range cfg.ProviderAccounts {
		if account.ProviderID == operation.providerID {
			current[providerSetupAccountKey(account.ProviderID, account.ID)] = account
		}
	}
	if len(before) == 0 {
		// New account: reject an existing identity, then append all after
		// snapshots (normally one account) and reconcile generated routes.
		for _, account := range after {
			key := providerSetupAccountKey(account.ProviderID, account.ID)
			if _, exists := current[key]; exists {
				return &providerSetupConflictError{field: field}
			}
			cfg.ProviderAccounts = append(cfg.ProviderAccounts, cloneProviderSetupAccount(account))
		}
	} else {
		for _, account := range before {
			key := providerSetupAccountKey(account.ProviderID, account.ID)
			got, exists := current[key]
			if !exists || !reflect.DeepEqual(got, account) {
				return &providerSetupConflictError{field: field}
			}
		}
		beforeKeys := make(map[string]bool, len(before))
		for _, account := range before {
			beforeKeys[providerSetupAccountKey(account.ProviderID, account.ID)] = true
		}
		afterByKey := make(map[string]config.ProviderAccount, len(after))
		for _, account := range after {
			afterByKey[providerSetupAccountKey(account.ProviderID, account.ID)] = cloneProviderSetupAccount(account)
		}
		seenAfter := make(map[string]bool, len(after))
		out := cfg.ProviderAccounts[:0]
		for _, account := range cfg.ProviderAccounts {
			key := providerSetupAccountKey(account.ProviderID, account.ID)
			if account.ProviderID == operation.providerID && beforeKeys[key] {
				if replacement, exists := afterByKey[key]; exists {
					out = append(out, replacement)
					seenAfter[key] = true
				}
				continue
			}
			out = append(out, account)
		}
		for _, account := range after {
			key := providerSetupAccountKey(account.ProviderID, account.ID)
			if !seenAfter[key] {
				out = append(out, cloneProviderSetupAccount(account))
			}
		}
		cfg.ProviderAccounts = out
	}
	if _, _, reconcileErr := config.ReconcileProviderAccounts(cfg); reconcileErr != nil {
		return fmt.Errorf("replay %s: %w", field, reconcileErr)
	}
	return nil
}

func replayProviderAccountDefaultModel(cfg *config.Config, operation providerSetupOperation) error {
	if operation.accountDefaultModelChanged {
		if cfg.DefaultModel != operation.beforeString {
			return &providerSetupConflictError{field: "default_model"}
		}
		if err := cfg.SetDefaultModel(operation.afterString); err != nil {
			return fmt.Errorf("replay account default_model: %w", err)
		}
	}
	return nil
}

func replayProviderAccountRoute(cfg *config.Config, operation providerSetupOperation, field string) error {
	if operation.routeID == "" || operation.afterRoute == nil || len(operation.beforeAccounts) != 0 || len(operation.afterAccounts) != 0 {
		return nil
	}
	_, account, found := cfgAccountByIdentity(cfg, operation.providerID, operation.accountID)
	if !found {
		return &providerSetupConflictError{field: field}
	}
	if operation.beforeRoute != nil && accountRouteEnabled(account, operation.routeID) != *operation.beforeRoute {
		return &providerSetupConflictError{field: field + ".route"}
	}
	if err := cfg.SetProviderAccountRouteEnabled(operation.providerID, operation.accountID, operation.routeID, *operation.afterRoute); err != nil {
		return fmt.Errorf("replay %s route: %w", field, err)
	}
	return nil
}

func cfgAccountByIdentity(cfg *config.Config, providerID, accountID string) (int, config.ProviderAccount, bool) {
	for i, account := range cfg.ProviderAccounts {
		if account.ProviderID == providerID && account.ID == accountID {
			return i, account, true
		}
	}
	return -1, config.ProviderAccount{}, false
}

func accountRouteEnabled(account config.ProviderAccount, routeID string) bool {
	for _, disabled := range account.DisabledRoutes {
		if strings.TrimSpace(disabled) == strings.TrimSpace(routeID) {
			return false
		}
	}
	return true
}

func providerSetupAccessContains(names []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, name := range names {
		if strings.TrimSpace(name) == want {
			return true
		}
	}
	return false
}
