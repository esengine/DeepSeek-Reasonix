package termrender

import (
	"os"

	"golang.org/x/term"

	"reasonix/internal/contract/config"
)

// ConfigureThemeFromConfig applies the [ui] theme, style and cursor shape from
// the user's config, falling back to an auto-detected theme when none loads.
func ConfigureThemeFromConfig() {
	if cfg, err := config.Load(); err == nil {
		configureThemeWithStyle(cfg.UITheme(), cfg.UIThemeStyle())
		cursorShape = cfg.UICursorShape()
		configureDiffFormatter(cfg)
		configureDiffFences(cfg)
	} else {
		ConfigureTheme("auto")
		cursorShape = "bar"
		configureDiffFormatter(nil)
		configureDiffFences(nil)
	}
}

// ConfigureThemeFromConfigForTTYOutput is ConfigureThemeFromConfig that may
// probe the terminal background. Call it only before anything else reads
// stdin: the probe puts stdin in raw mode.
func ConfigureThemeFromConfigForTTYOutput() {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		withTerminalProbe(ConfigureThemeFromConfig)
		return
	}
	ConfigureThemeFromConfig()
}
