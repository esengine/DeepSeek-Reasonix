package persona

import "reasonix/internal/contract/config"

// Settings is a persona resolved for composition: the config value normalized,
// with the tri-state runtime-context pointer already resolved to a bool.
type Settings struct {
	Prefix                string
	Suffix                string
	Complete              bool
	IncludeRuntimeContext bool
}

// FromConfig resolves the stored persona. Normalization here is what makes an
// old config (no persona at all) compose exactly like one with an empty table.
func FromConfig(c config.PersonaConfig) Settings {
	p := c.Normalize()
	return Settings{
		Prefix:                p.Prefix,
		Suffix:                p.Suffix,
		Complete:              p.Complete,
		IncludeRuntimeContext: p.RuntimeContextEnabled(),
	}
}

// ApplyPrefix puts the identity ahead of base. Complete mode is not a prefix
// addition — Final replaces the prompt with the prefix — so this returns base
// unchanged there, letting the caller keep assembling the sections the runtime
// still needs.
func ApplyPrefix(base string, s Settings) string {
	if s.Complete || s.Prefix == "" {
		return base
	}
	if base == "" {
		return s.Prefix
	}
	return s.Prefix + "\n\n" + base
}

// ApplySuffix trails the first-party guidance with the user's suffix, and
// likewise does nothing in complete mode.
func ApplySuffix(base string, s Settings) string {
	if s.Complete || s.Suffix == "" {
		return base
	}
	if base == "" {
		return s.Suffix
	}
	return base + "\n\n" + s.Suffix
}

// Final returns the prompt the provider sees. In complete mode that is exactly
// the trimmed prefix, which is the persona contract: every other section is
// suppressed, while the memory and skill stores built alongside remain live.
func Final(s Settings, assembled string) string {
	if s.Complete {
		return s.Prefix
	}
	return assembled
}
