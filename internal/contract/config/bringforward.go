package config

// bringConfigForward applies every in-memory upgrade a loaded config gets:
// retired keys normalised, shipped shapes moved forward, prices backfilled.
// One list, because the edit path has to apply exactly the same set.
func bringConfigForward(cfg *Config) {
	normalizeLegacyMCPTiers(cfg)
	normalizeLegacyStepFunBaseURLs(cfg)
	migrateSystemOneToDecisionRole(cfg)
	upgradeShippedPresets(cfg)
	normalizeLegacyMimoCustomProviders(cfg)
	normalizeLegacyProviderFields(cfg)
	normalizeDesktopOfficialProviderAccess(cfg)
	normalizeOfficialDeepSeekModels(cfg)
	migrateBillingDisplayCurrency(cfg)
	freezeProviderBillingCurrencies(cfg)
	applyOfficialDefaultPricing(cfg)
	backfillDeepSeekOfficialPrices(cfg)
	normalizeEffortConfig(cfg)
	backfillDeepSeekPro(cfg)
}
