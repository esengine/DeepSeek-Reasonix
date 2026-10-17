package config

import (
	"errors"
	"fmt"
	"strings"
)

// PersonaConfig is the user's system persona. The zero value means "no
// persona": the prompt is assembled exactly as if this feature did not exist,
// which is what keeps configs written before it readable.
type PersonaConfig struct {
	Prefix string `toml:"prefix"`
	Suffix string `toml:"suffix"`
	// Complete uses Prefix as the whole system prompt. Every other section is
	// suppressed, tools stay available.
	Complete bool `toml:"complete"`
	// IncludeRuntimeContext is a pointer so absent (nil) means true, matching
	// zero-value-is-no-persona; explicit false suppresses the probed environment
	// section and the offline note.
	IncludeRuntimeContext *bool `toml:"include_runtime_context"`
}

const (
	// maxPersonaPartBytes bounds each half; the total is bounded separately so a
	// future field cannot turn two permitted halves into an unbounded prefix.
	maxPersonaPartBytes  = 32 << 10
	maxPersonaTotalBytes = 64 << 10
)

var (
	// ErrPersonaCompleteRequiresPrefix marks complete mode without an identity:
	// suppressing every other section would leave the model with nothing.
	ErrPersonaCompleteRequiresPrefix = errors.New("persona complete mode requires a prefix")
	// ErrPersonaTooLarge marks a persona over the prefix/suffix size caps.
	ErrPersonaTooLarge = errors.New("persona is too large")
)

// Normalize trims the bodies and collapses a persona that contributes nothing,
// so an empty [agent.persona] table and an absent one are the same config. An
// explicit include_runtime_context is a contribution even with no text: it is a
// standalone switch, and collapsing it would make the save read back unchanged.
func (p PersonaConfig) Normalize() PersonaConfig {
	p.Prefix = strings.TrimSpace(p.Prefix)
	p.Suffix = strings.TrimSpace(p.Suffix)
	if p.Prefix == "" && p.Suffix == "" && !p.Complete && p.IncludeRuntimeContext == nil {
		return PersonaConfig{}
	}
	return p
}

// Validate rejects the two personas that cannot be honored: complete mode with
// no prefix (it would erase the whole prompt) and bodies past the size caps.
func (p PersonaConfig) Validate() error {
	p = p.Normalize()
	if p.Complete && p.Prefix == "" {
		return fmt.Errorf("%w: complete mode needs a non-empty prefix", ErrPersonaCompleteRequiresPrefix)
	}
	if len(p.Prefix) > maxPersonaPartBytes || len(p.Suffix) > maxPersonaPartBytes || len(p.Prefix)+len(p.Suffix) > maxPersonaTotalBytes {
		return fmt.Errorf("%w: prefix and suffix must each be at most 32 KiB", ErrPersonaTooLarge)
	}
	return nil
}

// RuntimeContextEnabled reports whether optional runtime context may enter the
// prompt. Absent means enabled, so only an explicit false suppresses it.
func (p PersonaConfig) RuntimeContextEnabled() bool {
	return p.IncludeRuntimeContext == nil || *p.IncludeRuntimeContext
}

// IsZero reports whether the persona contributes anything, after trimming.
// Complete without a prefix is not zero: the invalid config is preserved so the
// user can repair it rather than having it silently disappear.
func (p PersonaConfig) IsZero() bool {
	p = p.Normalize()
	return p.Prefix == "" && p.Suffix == "" && !p.Complete && p.IncludeRuntimeContext == nil
}

// SetPersona normalizes and stores the user's system persona. Persistence picks
// the user config; the running session keeps the prompt it was built with.
func (c *Config) SetPersona(p PersonaConfig) error {
	p = p.Normalize()
	if err := p.Validate(); err != nil {
		return err
	}
	c.Agent.Persona = p
	return nil
}

// renderPersonaTable writes the [agent.persona] table. Absent fields are not
// written, so a field the user never touched round-trips as absent; the
// pointer-only field is written only when the user made a choice.
func renderPersonaTable(b *strings.Builder, p PersonaConfig) {
	b.WriteString("\n[agent.persona]\n")
	if p.Prefix != "" {
		b.WriteString("prefix = " + tomlMultilineBasicString(p.Prefix) + "\n")
	}
	if p.Suffix != "" {
		b.WriteString("suffix = " + tomlMultilineBasicString(p.Suffix) + "\n")
	}
	if p.Complete {
		b.WriteString("complete = true   # use prefix as the whole system prompt; every other section is suppressed\n")
	}
	if p.IncludeRuntimeContext != nil {
		fmt.Fprintf(b, "include_runtime_context = %v   # false omits the probed environment section and offline note\n", *p.IncludeRuntimeContext)
	}
}
