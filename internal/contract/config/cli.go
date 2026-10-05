package config

// CLIConfig controls user-global native CLI behavior. It is separate from
// project runtime settings so a repository cannot change the installed
// binary's update channel, run an external diff formatter, or switch the
// diff renderer.
type CLIConfig struct {
	// UpdateChannel is decoded for compatibility with pre-single-channel
	// configurations. Runtime behavior is always the official release channel,
	// and the canonical renderer intentionally drops this field.
	UpdateChannel string `toml:"update_channel"`
	// DiffFormatter is an optional external command (argv, no shell) that formats
	// a diff fence, card, or diff-only shell result, e.g. "delta"; empty uses the
	// built-in renderer. User-global only; ./reasonix.toml cannot set it.
	DiffFormatter string `toml:"diff_formatter"`
	// DiffFences opts into rendering a fenced diff/patch block through the
	// colourised diff renderer instead of the plain code rail. Default false;
	// user-global only, so ./reasonix.toml cannot set it.
	DiffFences bool `toml:"diff_fences"`
}
