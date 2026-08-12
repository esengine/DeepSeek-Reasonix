package boot

import (
	"reasonix/internal/config"
)

func handleConfigLoadWarnings(opts Options, cfg *config.Config) bool {
	if cfg == nil || !cfg.HasLoadWarnings() || opts.OnConfigLoadWarnings == nil {
		return false
	}
	return opts.OnConfigLoadWarnings(cfg.LoadWarnings())
}

func deepSeekProtocolMigrationNoticeError(configLoadWarningsHandled bool, err error) error {
	// Suppress only after a frontend explicitly accepts the resilient-loader
	// warnings. StatsSource is only a usage label and does not prove that its
	// caller can present Config.LoadWarnings.
	if configLoadWarningsHandled && config.IsDeepSeekProtocolConfigParseError(err) {
		return nil
	}
	return err
}
