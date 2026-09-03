package config

type userGlobalConfigPins struct {
	CLI              CLIConfig
	Secrets          SecretsConfig
	Remote           RemoteConfig
	DesktopLanguage  string
	PricingCurrency  string
	BillingCurrency  string
	Telemetry        TelemetryConfig
	LegacyAnchor     bool
	ProviderAccounts []ProviderAccount
}

func captureUserGlobalPins(cfg *Config) userGlobalConfigPins {
	return userGlobalConfigPins{
		CLI:              cfg.CLI,
		Secrets:          cfg.Secrets,
		Remote:           cfg.Remote.Clone(),
		DesktopLanguage:  cfg.Desktop.Language,
		PricingCurrency:  cfg.Desktop.Currency,
		BillingCurrency:  cfg.Billing.DisplayCurrency,
		Telemetry:        cfg.Telemetry,
		LegacyAnchor:     cfg.Agent.LegacyAnchorSafetyGate,
		ProviderAccounts: cloneProviderAccounts(cfg.ProviderAccounts),
	}
}

func applyUserGlobalPins(cfg *Config, pins userGlobalConfigPins, projectPath string, projectDeclaredAccounts bool) {
	cfg.CLI = pins.CLI
	cfg.Secrets = pins.Secrets
	cfg.Remote = pins.Remote
	cfg.Desktop.Language = pins.DesktopLanguage
	cfg.Desktop.Currency = pins.PricingCurrency
	cfg.Billing.DisplayCurrency = pins.BillingCurrency
	cfg.Telemetry, cfg.Agent.LegacyAnchorSafetyGate = pins.Telemetry, pins.LegacyAnchor
	if projectDeclaredAccounts {
		cfg.addLoadWarning("project config " + projectPath + " declared provider_accounts; provider accounts are user-global and were ignored")
	}
	cfg.ProviderAccounts = pins.ProviderAccounts
}
