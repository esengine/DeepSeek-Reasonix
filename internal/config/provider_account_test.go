package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestProviderAccountIDValidation(t *testing.T) {
	t.Parallel()
	cfg := Default()
	if _, err := cfg.AddProviderAccount("", "", "主账号", "DEEPSEEK_API_KEY"); err == nil {
		t.Fatal("empty provider_id must be rejected")
	}
	if err := validateProviderAccount(ProviderAccount{ProviderID: "deepseek", ID: "Main", Label: "Main", APIKeyEnv: "DEEPSEEK_API_KEY"}); err == nil {
		t.Fatal("uppercase ID must be rejected")
	}
	if err := validateProviderAccount(ProviderAccount{ProviderID: "deepseek", ID: "main", Label: "Main", APIKeyEnv: "not a key"}); err == nil {
		t.Fatal("invalid api_key_env must be rejected")
	}
	dup := Default()
	if _, err := dup.AddProviderAccount("deepseek", "", "主账号", "DEEPSEEK_API_KEY"); err != nil {
		t.Fatalf("add main: %v", err)
	}
	if err := validateProviderAccount(ProviderAccount{ProviderID: "deepseek", ID: "main", Label: "Main", APIKeyEnv: "DEEPSEEK_API_KEY"}); err != nil {
		t.Fatalf("valid account rejected: %v", err)
	}
	warnings := normalizeProviderAccountList(&Config{ProviderAccounts: []ProviderAccount{
		{ProviderID: "deepseek", ID: "main", Label: "A", APIKeyEnv: "DEEPSEEK_API_KEY", Default: true},
		{ProviderID: "deepseek", ID: "main", Label: "B", APIKeyEnv: "DEEPSEEK_API_KEY_B"},
	}})
	if len(warnings) == 0 {
		t.Fatal("duplicate IDs must warn")
	}
}

func TestProviderAccountSlugStabilityAndFamilies(t *testing.T) {
	t.Parallel()
	if got := SuggestProviderAccountID("deepseek", "团队账号"); got != "team" {
		t.Fatalf("deepseek 团队账号 slug = %q, want team", got)
	}
	if got := SuggestProviderAccountID("opencode-go", "团队账号"); got != "team" {
		t.Fatalf("opencode-go 团队账号 slug = %q, want team", got)
	}
	first := SuggestProviderAccountID("deepseek", "自定义网关")
	second := SuggestProviderAccountID("deepseek", "自定义网关")
	if first != second || !IsProviderAccountID(first) {
		t.Fatalf("unstable slug %q vs %q", first, second)
	}
	if SuggestProviderAccountID("deepseek", "主账号") != MainProviderAccountID {
		t.Fatal("主账号 should map to main")
	}
}

func TestCuratedPresetsHaveAccountGroupID(t *testing.T) {
	t.Parallel()
	for _, preset := range CuratedProviderPresets() {
		if strings.TrimSpace(preset.AccountGroupID) == "" {
			t.Fatalf("preset %q missing AccountGroupID", preset.ID)
		}
	}
	deepseek, ok := CuratedProviderPreset("deepseek-responses")
	if !ok || deepseek.AccountGroupID != "deepseek" {
		t.Fatalf("deepseek-responses group = %q", deepseek.AccountGroupID)
	}
	goChat, _ := CuratedProviderPreset("opencode-go")
	goAnth, _ := CuratedProviderPreset("opencode-go-anthropic")
	goResp, _ := CuratedProviderPreset("opencode-go-responses")
	if goChat.AccountGroupID != "opencode-go" || goAnth.AccountGroupID != "opencode-go" || goResp.AccountGroupID != "opencode-go" {
		t.Fatalf("opencode-go family grouping failed: %q %q %q", goChat.AccountGroupID, goAnth.AccountGroupID, goResp.AccountGroupID)
	}
}

func TestProviderAccountTOMLOmitsSecrets(t *testing.T) {
	cfg := Default()
	account, err := cfg.AddProviderAccount("deepseek", "", "团队账号", "DEEPSEEK_API_KEY_TEAM")
	if err != nil {
		t.Fatal(err)
	}
	if account.ID != "team" {
		t.Fatalf("account id = %q, want team", account.ID)
	}
	raw := RenderTOML(cfg)
	if strings.Contains(raw, "sk-") || strings.Contains(strings.ToLower(raw), "sk-secret") {
		t.Fatalf("rendered TOML leaked a secret-looking value:\n%s", raw)
	}
	if !strings.Contains(raw, "api_key_env = \"DEEPSEEK_API_KEY_TEAM\"") {
		t.Fatalf("missing account env in TOML:\n%s", raw)
	}
}

func TestProviderAccountDisabledRoutesRenderNormalized(t *testing.T) {
	cfg := &Config{ProviderAccounts: []ProviderAccount{{
		ProviderID: "opencode-go", ID: "team", Label: "Team", APIKeyEnv: "TEAM_KEY",
		DisabledRoutes: []string{" opencode-go-responses ", "opencode-go-responses", "opencode-go-anthropic"},
	}}}
	normalizeProviderAccountList(cfg)
	if got, want := cfg.ProviderAccounts[0].DisabledRoutes, []string{"opencode-go-anthropic", "opencode-go-responses"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("disabled routes = %v, want %v", got, want)
	}
	raw := RenderTOML(cfg)
	if !strings.Contains(raw, `disabled_routes = ["opencode-go-anthropic", "opencode-go-responses"]`) {
		t.Fatalf("render missing normalized disabled routes:\n%s", raw)
	}
}

func TestProviderAccountDisabledRoutesRoundTripWithoutResurrection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	cfg := Default()
	if _, err := cfg.AddProviderAccount("opencode-go", "opencode-go-recommended", "Main", "OPENCODE_MAIN_KEY"); err != nil {
		t.Fatal(err)
	}
	account, err := cfg.AddProviderAccount("opencode-go", "opencode-go-recommended", "Team", "OPENCODE_TEAM_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetProviderAccountRouteEnabled(account.ProviderID, account.ID, "opencode-go-responses", false); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetProviderAccountRouteEnabled(account.ProviderID, account.ID, "opencode-go-responses", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Provider("opencode-go-responses--team"); !ok {
		t.Fatal("restored route provider entry missing")
	}
	if err := cfg.SetProviderAccountRouteEnabled(account.ProviderID, account.ID, "opencode-go-responses", false); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	reloaded := LoadForEditWithoutCredentials(path)
	_, team, ok := reloaded.lookupProviderAccount("opencode-go", "team")
	if !ok || !providerAccountRouteDisabled(team, "opencode-go-responses") {
		t.Fatalf("reloaded team account = %+v", team)
	}
	if _, ok := reloaded.Provider("opencode-go-responses--team"); !ok {
		t.Fatal("reloaded config lost retained disabled route entry")
	}
	if _, ok := reloaded.ResolveModel("opencode-go--team/glm-5.2"); !ok {
		t.Fatal("reloaded config lost explicit account model")
	}
}

func TestExpandOpenCodeGoAndDeepSeekAccounts(t *testing.T) {
	cfg := Default()
	main, err := cfg.AddProviderAccount("opencode-go", "opencode-go-recommended", "主账号", "OPENCODE_GO_API_KEY")
	if err != nil {
		t.Fatalf("add opencode main: %v", err)
	}
	entries, err := ExpandProviderAccount(cfg, main)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
		if e.APIKeyEnv != "OPENCODE_GO_API_KEY" {
			t.Fatalf("entry %q env = %q", e.Name, e.APIKeyEnv)
		}
	}
	for _, want := range []string{"opencode-go", "opencode-go-anthropic", "opencode-go-responses"} {
		if !names[want] {
			t.Fatalf("missing OpenCode Go route %q in %v", want, names)
		}
	}
	team, err := cfg.AddProviderAccount("opencode-go", "opencode-go-recommended", "团队账号", "OPENCODE_GO_API_KEY_TEAM")
	if err != nil {
		t.Fatalf("add opencode team: %v", err)
	}
	teamEntries, err := ExpandProviderAccount(cfg, team)
	if err != nil {
		t.Fatal(err)
	}
	teamNames := map[string]string{}
	for _, e := range teamEntries {
		teamNames[e.AccountRouteID] = e.Name
		if e.APIKeyEnv != "OPENCODE_GO_API_KEY_TEAM" {
			t.Fatalf("team entry %q env = %q", e.Name, e.APIKeyEnv)
		}
		if e.Name == "opencode-go" || e.Name == "opencode-go-anthropic" {
			t.Fatalf("team account reused main provider name %q", e.Name)
		}
	}
	if teamNames["opencode-go"] != "opencode-go--team" {
		t.Fatalf("team chat name = %q", teamNames["opencode-go"])
	}
	deepTeam, err := cfg.AddProviderAccount("deepseek", "", "团队账号", "DEEPSEEK_API_KEY_TEAM")
	if err != nil {
		t.Fatal(err)
	}
	deepEntries, err := ExpandProviderAccount(cfg, deepTeam)
	if err != nil {
		t.Fatal(err)
	}
	if len(deepEntries) == 0 {
		t.Fatal("deepseek team produced no routes")
	}
	foundCombined := false
	for _, e := range deepEntries {
		if e.Name == "deepseek--team" && e.Kind == "anthropic" {
			foundCombined = true
		}
		if e.APIKeyEnv != "DEEPSEEK_API_KEY_TEAM" {
			t.Fatalf("deepseek team env = %q", e.APIKeyEnv)
		}
	}
	if !foundCombined {
		t.Fatalf("deepseek team missing combined route: %+v", deepEntries)
	}
}

func TestReconcilePreservesUserEditsAndIsIdempotent(t *testing.T) {
	cfg := Default()
	ensureProviderAccounts(cfg)
	idx := providerIndexByName(cfg, "deepseek-flash")
	if idx < 0 {
		t.Fatal("missing deepseek-flash")
	}
	cfg.Providers[idx].BaseURL = "https://proxy.example/anthropic"
	cfg.Providers[idx].Headers = map[string]string{"X-Route": "custom"}
	cfg.Providers[idx].Models = []string{"deepseek-v4-flash"}
	cfg.Providers[idx].RequestURL = "https://proxy.example/anthropic/v1/messages"
	changed, _, err := ReconcileProviderAccounts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := cfg.Provider("deepseek-flash")
	if !ok {
		t.Fatal("deepseek-flash disappeared")
	}
	if got.BaseURL != "https://proxy.example/anthropic" || got.RequestURL != "https://proxy.example/anthropic/v1/messages" || got.Headers["X-Route"] != "custom" {
		t.Fatalf("user fields overwritten: %+v", got)
	}
	if len(got.Models) != 1 || got.Models[0] != "deepseek-v4-flash" {
		t.Fatalf("models overwritten: %v", got.Models)
	}
	again, _, err := ReconcileProviderAccounts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = changed // first reconcile may stamp metadata; the second must be stable.
	if again {
		t.Fatal("reconcile is not idempotent")
	}
}

func TestLegacyProviderAccountsMigrateToMain(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REASONIX_HOME", dir)
	path := filepath.Join(dir, "config.toml")
	body := `config_version = 7
default_model = "deepseek-flash/deepseek-v4-flash"

[[providers]]
name = "deepseek-flash"
kind = "anthropic"
base_url = "https://api.deepseek.com/anthropic"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"

[[providers]]
name = "deepseek-pro"
kind = "anthropic"
base_url = "https://api.deepseek.com/anthropic"
model = "deepseek-v4-pro"
api_key_env = "DEEPSEEK_API_KEY"

[[providers]]
name = "gateway"
kind = "openai"
base_url = "http://localhost:8021/v1"
models = ["my-model"]
api_key_env = "GATEWAY_API_KEY"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadForEditWithoutCredentials(path)
	if len(cfg.ProviderAccounts) != 1 {
		t.Fatalf("accounts = %+v, want one main DeepSeek account", cfg.ProviderAccounts)
	}
	account := cfg.ProviderAccounts[0]
	if account.ProviderID != "deepseek" || account.ID != MainProviderAccountID || account.APIKeyEnv != "DEEPSEEK_API_KEY" {
		t.Fatalf("migrated account = %+v", account)
	}
	flash, _ := cfg.ResolveModel("deepseek-flash/deepseek-v4-flash")
	if flash == nil || flash.Name != "deepseek-flash" {
		t.Fatalf("old ref resolved to %+v", flash)
	}
	if _, ok := cfg.Provider("gateway"); !ok {
		t.Fatal("custom provider was dropped")
	}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "[[provider_accounts]]") || !strings.Contains(text, `id          = "main"`) {
		t.Fatalf("saved config missing provider_accounts:\n%s", text)
	}
	if !strings.Contains(text, "name        = \"deepseek-flash\"") {
		t.Fatalf("saved config renamed old provider:\n%s", text)
	}
	reloaded := LoadForEditWithoutCredentials(path)
	if len(reloaded.ProviderAccounts) != 1 || reloaded.ProviderAccounts[0].ID != MainProviderAccountID {
		t.Fatalf("reload accounts = %+v", reloaded.ProviderAccounts)
	}
	if _, ok := reloaded.ResolveModel("deepseek-flash/deepseek-v4-flash"); !ok {
		t.Fatal("reload lost old model ref")
	}
}

func TestLegacyDifferentKeyEnvsBecomeMultipleAccounts(t *testing.T) {
	cfg := &Config{Providers: []ProviderEntry{
		{Name: "deepseek-flash", Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL, Model: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY"},
		{Name: "deepseek-pro", Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL, Model: "deepseek-v4-pro", APIKeyEnv: "DEEPSEEK_API_KEY_WORK"},
	}}
	if !inferProviderAccounts(cfg) {
		t.Fatal("expected migration")
	}
	if len(cfg.ProviderAccounts) != 2 {
		t.Fatalf("accounts = %+v, want 2", cfg.ProviderAccounts)
	}
	if cfg.ProviderAccounts[0].ID != MainProviderAccountID {
		t.Fatalf("first account id = %q", cfg.ProviderAccounts[0].ID)
	}
	if !strings.HasPrefix(cfg.ProviderAccounts[1].ID, legacyAccountIDPrefix) {
		t.Fatalf("second account id = %q, want legacy-*", cfg.ProviderAccounts[1].ID)
	}
}

func TestProjectConfigCannotDefineProviderAccounts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	user := filepath.Join(home, "config.toml")
	if err := os.WriteFile(user, []byte("config_version = 8\ndefault_model = \"deepseek-flash\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	project := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(project, []byte(`
[[provider_accounts]]
provider_id = "deepseek"
id = "sneaky"
label = "Project"
api_key_env = "PROJECT_DEEPSEEK_KEY"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootWithoutCredentialsReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range cfg.ProviderAccounts {
		if account.ID == "sneaky" {
			t.Fatal("project provider_accounts leaked into runtime config")
		}
	}
	warnings := cfg.LoadWarnings()
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "provider_accounts") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing project account warning: %v", warnings)
	}
}

func TestResolveFamilyAndExplicitAccountModel(t *testing.T) {
	cfg := Default()
	ensureProviderAccounts(cfg)
	if _, err := cfg.AddProviderAccount("deepseek", "", "团队账号", "DEEPSEEK_API_KEY_TEAM"); err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Providers {
		if cfg.Providers[i].APIKeyEnv == "DEEPSEEK_API_KEY" {
			cfg.Providers[i].resolvedAPIKey = "sk-main"
		}
		if cfg.Providers[i].APIKeyEnv == "DEEPSEEK_API_KEY_TEAM" {
			cfg.Providers[i].resolvedAPIKey = "sk-team"
		}
	}
	family, ok := cfg.ResolveModel("deepseek")
	if !ok {
		t.Fatal("family deepseek did not resolve")
	}
	if family.AccountID != MainProviderAccountID && family.Name != "deepseek-flash" && family.Name != "deepseek-pro" {
		t.Fatalf("family resolved to %+v", family)
	}
	team, ok := cfg.ResolveModel("deepseek--team/deepseek-v4-flash")
	if !ok {
		t.Fatal("explicit team ref did not resolve")
	}
	if team.AccountID != "team" || team.APIKeyEnv != "DEEPSEEK_API_KEY_TEAM" {
		t.Fatalf("team ref = %+v", team)
	}
	if err := cfg.SetProviderAccountEnabled("deepseek", "team", false); err != nil {
		t.Fatal(err)
	}
	if cfg.AccountEnabled("deepseek", "team") {
		t.Fatal("disabled account still enabled")
	}
	if _, ok := cfg.ResolveModel("deepseek--team/deepseek-v4-flash"); !ok {
		t.Fatal("explicit disabled-account ref must still resolve")
	}
	ref, _, ok := cfg.ResolveNewSessionChatModel()
	if !ok {
		t.Fatal("new session model missing")
	}
	if strings.Contains(ref, "--team") {
		t.Fatalf("disabled account leaked into new session candidates: %s", ref)
	}
}

func TestCuratedIdentityDoesNotInferCustomEndpointByURL(t *testing.T) {
	cfg := &Config{Providers: []ProviderEntry{{
		Name:      "my-gateway",
		Kind:      "openai",
		BaseURL:   "https://api.deepseek.com",
		Model:     "custom-model",
		APIKeyEnv: "CUSTOM_KEY",
	}}}
	if inferProviderAccounts(cfg) {
		t.Fatal("custom endpoint was inferred as a curated account")
	}
	if len(cfg.ProviderAccounts) != 0 {
		t.Fatalf("provider accounts = %+v, want none", cfg.ProviderAccounts)
	}
	if _, _, _, ok := curatedProviderIdentity(cfg.Providers[0]); ok {
		t.Fatal("custom provider was classified as a curated family")
	}
}

func TestProviderAccountDisabledRoutesSurviveReconcileAndRestore(t *testing.T) {
	cfg := Default()
	if _, err := cfg.AddProviderAccount("opencode-go", "opencode-go-recommended", "Main", "OPENCODE_MAIN_KEY"); err != nil {
		t.Fatal(err)
	}
	account, err := cfg.AddProviderAccount("opencode-go", "opencode-go-recommended", "Team", "OPENCODE_TEAM_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetProviderAccountRouteEnabled(account.ProviderID, account.ID, "opencode-go-responses", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Provider("opencode-go-responses--team"); !ok {
		t.Fatal("disabled route provider entry disappeared")
	}
	_, stored, ok := cfg.lookupProviderAccount(account.ProviderID, account.ID)
	if !ok || len(stored.DisabledRoutes) != 1 || stored.DisabledRoutes[0] != "opencode-go-responses" {
		t.Fatalf("disabled routes = %v accounts=%+v", stored.DisabledRoutes, cfg.ProviderAccounts)
	}
	if changed, _, err := ReconcileProviderAccounts(cfg); err != nil || changed {
		t.Fatalf("reconcile changed=%v err=%v", changed, err)
	}
	if _, ok := cfg.ResolveModel("opencode-go--team/glm-5.2"); !ok {
		t.Fatal("explicit account model should remain resolvable")
	}
	if err := cfg.RestoreProviderAccount(account.ProviderID, account.ID); err != nil {
		t.Fatal(err)
	}
	_, stored, _ = cfg.lookupProviderAccount(account.ProviderID, account.ID)
	if len(stored.DisabledRoutes) != 0 {
		t.Fatalf("restore left disabled routes: %v", stored.DisabledRoutes)
	}
}

func TestSetDefaultAccountUpdatesFamilyDefaultModel(t *testing.T) {
	cfg := Default()
	ensureProviderAccounts(cfg)
	if _, err := cfg.AddProviderAccount("deepseek", "", "Team", "DEEPSEEK_TEAM_KEY"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetProviderAccountDefault("deepseek", "team"); err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.ResolveModel(cfg.DefaultModel)
	if !ok || entry.AccountID != "team" {
		t.Fatalf("default model = %q, resolved entry = %+v", cfg.DefaultModel, entry)
	}
	if got, _, ok := cfg.ResolveNewSessionChatModel(); !ok || !strings.Contains(got, "--team/") {
		t.Fatalf("new session model = %q, want team account", got)
	}
}

func TestRetireProviderAccountDisablesAllRoutes(t *testing.T) {
	cfg := Default()
	if _, err := cfg.AddProviderAccount("opencode-go", "opencode-go-recommended", "Main", "OPENCODE_MAIN_KEY"); err != nil {
		t.Fatal(err)
	}
	account, err := cfg.AddProviderAccount("opencode-go", "opencode-go-recommended", "Team", "OPENCODE_TEAM_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.RetireProviderAccount(account.ProviderID, account.ID); err != nil {
		t.Fatal(err)
	}
	_, retired, _ := cfg.lookupProviderAccount(account.ProviderID, account.ID)
	if !retired.Retired || retired.IsEnabled() || len(retired.DisabledRoutes) == 0 {
		t.Fatalf("retired account = %+v", retired)
	}
	if _, ok := cfg.DefaultAccount(account.ProviderID); !ok {
		t.Fatal("main account should remain family default after team retirement")
	}
}
