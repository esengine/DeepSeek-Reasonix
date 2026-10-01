package config

import (
	"maps"

	"github.com/BurntSushi/toml"
)

// userGlobals are the tables a workspace file never gets to set: the user's
// values are restored over whatever the merge left.
type userGlobals struct {
	cli                                       CLIConfig
	secrets                                   SecretsConfig
	remote                                    RemoteConfig
	storage                                   map[string]string
	serve                                     ServeConfig
	desktopLanguage, currency, billingDisplay string
	telemetry                                 TelemetryConfig
	statusline                                StatuslineConfig
	progressWatch                             ProgressWatchConfig
	autoArchive                               AutoArchiveConfig
	commandMode                               string
}

func holdUserGlobals(c *Config) userGlobals {
	return userGlobals{
		cli: c.CLI, secrets: c.Secrets,
		remote: c.Remote.Clone(), storage: maps.Clone(c.Storage), serve: c.Serve,
		desktopLanguage: c.Desktop.Language, currency: c.Desktop.Currency, billingDisplay: c.Billing.DisplayCurrency,
		telemetry: c.Telemetry, statusline: c.Statusline, progressWatch: c.ProgressWatch, autoArchive: c.AutoArchive, commandMode: c.UI.CommandMode,
	}
}

// restore puts the user's globals back: what a repository must not choose is
// the update channel and formatter, secret protection, remote hosts, storage
// and serve authentication, regional display, telemetry and key bindings.
func (g userGlobals) restore(c *Config) {
	c.CLI, c.Secrets = g.cli, g.secrets
	c.Remote, c.Storage, c.Serve = g.remote, g.storage, g.serve
	c.Desktop.Language, c.Desktop.Currency, c.Billing.DisplayCurrency = g.desktopLanguage, g.currency, g.billingDisplay
	c.Telemetry, c.Statusline, c.ProgressWatch, c.UI.CommandMode = g.telemetry, g.statusline, g.progressWatch, g.commandMode
	c.AutoArchive = g.autoArchive
}

// mergeProjectTOML merges the workspace's reasonix.toml over cfg and restores
// the user's globals. A file that does not parse is left out of the sources, so
// later plugin and provider merges never trip over it.
func mergeProjectTOML(cfg *Config, merge func(*Config, string) (toml.MetaData, error), path string, sources []string) (toml.MetaData, []string, error) {
	globals := holdUserGlobals(cfg)
	meta, err := merge(cfg, path)
	if err == nil && meta.IsDefined("agent", "system_prompt_file") {
		cfg.systemPromptFileSource = promptFileSourceProject
	}
	globals.restore(cfg)
	if err != nil {
		return meta, sources, err
	}
	return meta, append(sources, path), nil
}
