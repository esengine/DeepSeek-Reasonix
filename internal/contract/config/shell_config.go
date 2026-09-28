package config

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
