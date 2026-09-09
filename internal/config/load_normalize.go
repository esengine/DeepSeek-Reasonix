package config

// normalizeLoadedConfig applies post-merge compatibility repairs and validates
// mode enums in their established order.
func normalizeLoadedConfig(cfg *Config) error {
	normalizePluginCommandLines(cfg)
	normalizeLegacyEffort(cfg)
	cfg.ignoredLegacyStepLimits = normalizeLegacyAgentStepLimits(cfg)
	normalizeRetiredAutoPlan(cfg)
	if err := validateCompletionValidationModes(cfg.Agent.CompletionValidation); err != nil {
		return err
	}
	normalizeLegacyMCPTiers(cfg)
	normalizeLegacyStepFunBaseURLs(cfg)
	normalizeLegacyLongCatContextWindows(cfg)
	normalizeLegacyQwenContextWindows(cfg)
	normalizeLegacyKimiK3Catalog(cfg)
	normalizeLegacyOpenCodeGoInstalls(cfg)
	normalizeLegacyMimoCustomProviders(cfg)
	normalizeLegacyProviderModels(cfg)
	normalizeDesktopOfficialProviderAccess(cfg)
	normalizeOfficialDeepSeekModels(cfg)
	migrateBillingDisplayCurrency(cfg)
	freezeProviderBillingCurrencies(cfg)
	applyDeepSeekOfficialDefaultPricing(cfg)
	backfillDeepSeekOfficialPrices(cfg)
	normalizeEffortConfig(cfg)
	backfillDeepSeekPro(cfg)
	return nil
}
