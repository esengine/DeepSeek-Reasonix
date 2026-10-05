package boot

import (
	"fmt"
	"io"

	"reasonix/internal/contract/config"
)

// handleConfigLoadWarnings puts the resilient loader's findings in front of
// whoever started this session. A configuration that could not be read is
// replaced in memory by the last good one or by the defaults — a different
// model, and none of the person's permission rules — so a session that says
// nothing is a session running as somebody else's.
func handleConfigLoadWarnings(opts Options, cfg *config.Config, stderr io.Writer) bool {
	if cfg == nil || !cfg.HasLoadWarnings() {
		return false
	}
	if opts.OnConfigLoadWarnings != nil {
		return opts.OnConfigLoadWarnings(cfg.LoadWarnings())
	}
	// Nobody took them, so the diagnostic stream carries them. The notice a
	// frontend would otherwise have been spared stays owed: it reaches a
	// window, and this does not.
	for _, warning := range cfg.LoadWarnings() {
		fmt.Fprintln(stderr, "warning:", warning)
	}
	return false
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
