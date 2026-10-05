package config

// UserShell is the [tools.shell] the user configured under this binding's home,
// read without any project reasonix.toml. A command that drives an agent over a
// checkout it must not take commands from resolves its interpreter here; an
// unreadable user config yields the zero value, which is auto-detection.
func (r Roots) UserShell() ShellConfig {
	cfg := r.defaultConfig()
	if uc := r.userConfigLoadPath(); uc != "" {
		if _, err := decodeTOMLFile(uc, cfg); err != nil {
			return ShellConfig{}
		}
	}
	return cfg.Tools.Shell
}
