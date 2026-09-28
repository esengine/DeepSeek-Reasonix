package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// ShellConfig chooses the interpreter the bash tool runs commands under. Prefer
// is "auto" (default — real bash when present, else PowerShell on Windows),
// "bash", or "powershell"/"pwsh" (force it; warn at startup and fall back to
// auto if absent). Path optionally points at a specific shell executable. Env
// adds KEY=value variables to every bash command's environment.
type ShellConfig struct {
	Prefer string            `toml:"prefer"`
	Path   string            `toml:"path"`
	Env    map[string]string `toml:"env"`
}

// warnShellEnvProblems reports [tools.shell] env the load could not honour: a
// project table it discarded, and entries that cannot become environment entries
// (the bash tool drops those, so they would silently never take effect).
func warnShellEnvProblems(cfg *Config, projectMeta toml.MetaData) {
	if projectMeta.IsDefined("tools", "shell", "env") {
		cfg.addLoadWarning("project reasonix.toml sets [tools.shell] env, which is user-global; it was ignored")
	}
	for _, k := range slices.Sorted(maps.Keys(cfg.Tools.Shell.Env)) {
		switch {
		case k == "":
			cfg.addLoadWarning("[tools.shell] env has an empty key; it has no effect")
		case strings.ContainsAny(k, "=\x00"):
			cfg.addLoadWarning(fmt.Sprintf("[tools.shell] env key %q cannot appear in an environment entry; it has no effect", k))
		}
		if strings.ContainsRune(cfg.Tools.Shell.Env[k], 0) {
			cfg.addLoadWarning(fmt.Sprintf("[tools.shell] env value for %q contains a NUL byte; a command given it will fail to start", k))
		}
	}
}
