package config

import "testing"

// The decision backend was its own config section with its own vendor fields.
// It becomes an ordinary source and a role, and an installed config has to
// arrive there without the person retyping an address or a key.
func TestSystemOneSettingsBecomeASourceAndARole(t *testing.T) {
	cfg := &Config{}
	cfg.Tools.SystemOne.BaseURL = "https://decide.example"
	cfg.Tools.SystemOne.Model = "system-one"
	cfg.Tools.SystemOne.APIKeyEnv = "MY_DECISION_KEY"

	if !migrateSystemOneToDecisionRole(cfg) {
		t.Fatal("a configured backend was not migrated")
	}
	entry, ok := cfg.Provider("typesafe")
	if !ok {
		t.Fatal("no source was written for the configured backend")
	}
	if entry.Kind != "typesafe" || entry.BaseURL != "https://decide.example" || entry.APIKeyEnv != "MY_DECISION_KEY" {
		t.Fatalf("source = %#v", entry)
	}
	if !entry.HasModel("system-one") {
		t.Fatalf("source lists %v, want the configured model", entry.ModelList())
	}
	if cfg.Agent.DecisionModel != "typesafe/system-one" {
		t.Fatalf("decision role = %q", cfg.Agent.DecisionModel)
	}
	if cfg.Tools.SystemOne.BaseURL != "" || cfg.Tools.SystemOne.APIKeyEnv != "" {
		t.Fatal("the retired section still carries the settings it handed over")
	}
	if migrateSystemOneToDecisionRole(cfg) {
		t.Fatal("the migration ran a second time on an already-migrated config")
	}
}

// The gateway was a second backend with its own client and its own form. It
// speaks the same wire at another address, so it arrives as another source.
func TestASelfHostedGatewayBecomesAnOrdinarySource(t *testing.T) {
	cfg := &Config{}
	cfg.Tools.SystemOne.Laya.HTTPBaseURL = "http://127.0.0.1:8000"
	cfg.Tools.SystemOne.Laya.HTTPAPIKeyEnv = "GATEWAY_KEY"
	cfg.Tools.SystemOne.Laya.Local = true
	cfg.Tools.SystemOne.Laya.Python = "python3"

	if !migrateSystemOneToDecisionRole(cfg) {
		t.Fatal("a configured gateway was not migrated")
	}
	entry, ok := cfg.Provider("laya")
	if !ok || entry.BaseURL != "http://127.0.0.1:8000" || entry.APIKeyEnv != "GATEWAY_KEY" {
		t.Fatalf("gateway source = %#v, %v", entry, ok)
	}
	if cfg.Agent.DecisionModel != "laya/auto" {
		t.Fatalf("decision role = %q", cfg.Agent.DecisionModel)
	}
	// The local runtime is not an endpoint, so it stays where it is.
	if !cfg.Tools.SystemOne.Laya.Local || cfg.Tools.SystemOne.Laya.Python != "python3" {
		t.Fatal("the local runtime was carried off with the gateway")
	}
}

// A source the person added themselves outranks anything a migration writes.
func TestMigrationLeavesANameThePersonAlreadyUsed(t *testing.T) {
	cfg := &Config{Providers: []ProviderEntry{{Name: "typesafe", Kind: "openai", BaseURL: "https://mine.example"}}}
	cfg.Tools.SystemOne.BaseURL = "https://decide.example"

	migrateSystemOneToDecisionRole(cfg)
	entry, _ := cfg.Provider("typesafe")
	if entry.BaseURL != "https://mine.example" || entry.Kind != "openai" {
		t.Fatalf("the person's own source was overwritten: %#v", entry)
	}
	if cfg.Agent.DecisionModel != "" {
		t.Fatalf("decision role = %q, want none: nothing was adopted", cfg.Agent.DecisionModel)
	}
}

// An unconfigured install has nothing to move and must not grow a source.
func TestMigrationDoesNothingWithoutAConfiguredBackend(t *testing.T) {
	cfg := &Config{}
	if migrateSystemOneToDecisionRole(cfg) || len(cfg.Providers) != 0 {
		t.Fatal("an empty config grew a decision source")
	}
}
