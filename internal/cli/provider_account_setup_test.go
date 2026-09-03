package cli

import (
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestProviderAccountSetupOperationReplayPersistsAdd(t *testing.T) {
	isolateUserConfig(t)
	path := config.UserConfigPath()
	initial := config.Default()
	if err := initial.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	working := config.LoadForEdit(path)
	s := newProviderSetupSessionForPath(working, path)
	account, err := working.AddProviderAccount("deepseek", "deepseek-responses", "Team", "TEAM_DEEPSEEK_KEY")
	if err != nil {
		t.Fatal(err)
	}
	s.recordProviderAccountMutation(account.ProviderID, account.ID, nil, providerSetupAccountPtr(account), "", nil, nil)
	s.addProviderAccess(entriesForAccount(t, working, account))
	if _, err := commitProviderSetupSession(s, path); err != nil {
		t.Fatal(err)
	}
	reloaded := config.LoadForEditWithoutCredentials(path)
	got, ok := reloadedAccount(reloaded, account.ProviderID, account.ID)
	if !ok || got.Label != "Team" {
		t.Fatalf("reloaded account = %+v, want Team", got)
	}
	if _, ok := reloaded.ResolveAccountProvider(account.ProviderID, account.ID); !ok {
		t.Fatalf("reloaded account routes missing")
	}
}

func TestProviderAccountSetupOperationReplayPersistsRenameDefaultAndRoute(t *testing.T) {
	isolateUserConfig(t)
	path := config.UserConfigPath()
	initial := config.Default()
	if _, err := initial.AddProviderAccount("deepseek", "deepseek-responses", "Main", "MAIN_KEY"); err != nil {
		t.Fatal(err)
	}
	if err := initial.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	working := config.LoadForEdit(path)
	s := newProviderSetupSessionForPath(working, path)
	team, err := working.AddProviderAccount("deepseek", "deepseek-responses", "Team", "TEAM_KEY")
	if err != nil {
		t.Fatal(err)
	}
	s.recordProviderAccountMutation(team.ProviderID, team.ID, nil, providerSetupAccountPtr(team), "", nil, nil)
	if err := s.mutateProviderAccount(team.ProviderID, team.ID, func() error {
		return working.RenameProviderAccount(team.ProviderID, team.ID, "Renamed")
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.mutateProviderAccount(team.ProviderID, team.ID, func() error {
		return working.SetProviderAccountDefault(team.ProviderID, team.ID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.setProviderAccountRouteEnabled(team.ProviderID, team.ID, "deepseek-responses", false); err != nil {
		t.Fatal(err)
	}
	if _, err := commitProviderSetupSession(s, path); err != nil {
		t.Fatal(err)
	}
	reloaded := config.LoadForEditWithoutCredentials(path)
	got, ok := reloadedAccount(reloaded, team.ProviderID, team.ID)
	if !ok || got.Label != "Renamed" || !got.Default || len(got.DisabledRoutes) != 1 || got.DisabledRoutes[0] != "deepseek-responses" {
		t.Fatalf("reloaded team account = %+v", got)
	}
	for _, entry := range reloaded.Providers {
		if entry.AccountProviderID == team.ProviderID && entry.AccountID == team.ID && entry.AccountRouteID == "deepseek-responses" && containsString(reloaded.Desktop.ProviderAccess, entry.Name) {
			t.Fatalf("disabled route %q remains in provider access", entry.Name)
		}
	}
	if !strings.Contains(reloaded.DefaultModel, "--team/") {
		t.Fatalf("default model did not follow account default: %q", reloaded.DefaultModel)
	}
	restoreSession := newProviderSetupSessionForPath(reloaded, path)
	if err := restoreSession.restoreProviderAccount(team.ProviderID, team.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := commitProviderSetupSession(restoreSession, path); err != nil {
		t.Fatal(err)
	}
	restored, ok := reloadedAccount(config.LoadForEditWithoutCredentials(path), team.ProviderID, team.ID)
	if !ok || len(restored.DisabledRoutes) != 0 || restored.Retired {
		t.Fatalf("restored account = %+v", restored)
	}
}

func TestCuratedAccountSetupPresetsCoverEveryGroupDeterministically(t *testing.T) {
	want := map[string]config.ProviderPreset{}
	for _, preset := range config.CuratedProviderPresets() {
		group := preset.AccountGroupID
		if group == "" {
			continue
		}
		current, ok := want[group]
		if !ok || accountSetupPresetLess(preset, current) {
			want[group] = preset
		}
	}
	got := curatedAccountSetupPresets()
	if len(got) != len(want) {
		t.Fatalf("setup presets = %d, want %d groups", len(got), len(want))
	}
	seen := map[string]bool{}
	for _, preset := range got {
		if seen[preset.AccountGroupID] {
			t.Fatalf("duplicate setup group %q", preset.AccountGroupID)
		}
		seen[preset.AccountGroupID] = true
		if preset.ID != want[preset.AccountGroupID].ID {
			t.Fatalf("group %q selected %q, want %q", preset.AccountGroupID, preset.ID, want[preset.AccountGroupID].ID)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].ID > got[i].ID {
			t.Fatalf("setup presets not sorted by ID: %q before %q", got[i-1].ID, got[i].ID)
		}
	}
}

func entriesForAccount(t *testing.T, cfg *config.Config, account config.ProviderAccount) []config.ProviderEntry {
	t.Helper()
	entries, ok := cfg.ResolveAccountProvider(account.ProviderID, account.ID)
	if !ok {
		t.Fatalf("account %s/%s routes missing", account.ProviderID, account.ID)
	}
	return entries
}

func reloadedAccount(cfg *config.Config, providerID, accountID string) (config.ProviderAccount, bool) {
	for _, account := range cfg.ProviderAccounts {
		if account.ProviderID == providerID && account.ID == accountID {
			return account, true
		}
	}
	return config.ProviderAccount{}, false
}
