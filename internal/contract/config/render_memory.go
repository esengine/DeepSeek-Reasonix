package config

import (
	"fmt"
	"strings"
)

// renderSkillsConfig writes the [skills] section.
func renderSkillsConfig(b *strings.Builder, c *Config) {
	b.WriteString("[skills]\n")
	if len(c.Skills.Paths) > 0 {
		fmt.Fprintf(b, "paths = %s   # extra custom skill roots\n", renderStringArray(c.Skills.Paths))
	} else {
		b.WriteString("# paths = [\"~/my-skills\", \"../shared/skills\"]   # extra custom skill roots\n")
	}
	if len(c.Skills.ExcludedPaths) > 0 {
		fmt.Fprintf(b, "excluded_paths = %s   # skill roots hidden from discovery\n", renderStringArray(c.Skills.ExcludedPaths))
	} else {
		b.WriteString("# excluded_paths = [\"~/.agents/skills\"]   # hide convention roots without deleting folders\n")
	}
	if c.Skills.DisableImplicitInvocation {
		b.WriteString("disable_implicit_invocation = true   # keep /skill explicit; hide skill discovery and tools from the model\n")
	} else {
		b.WriteString("# disable_implicit_invocation = false   # keep skills available for automatic model invocation\n")
	}
	if c.Skills.SuppressWarnings {
		b.WriteString("suppress_warnings = true   # quiet non-fatal skill loading warnings; doctor diagnostics remain available\n")
	} else {
		b.WriteString("# suppress_warnings = false   # show non-fatal skill loading warnings\n")
	}
	if c.Skills.MaxDepth != 0 {
		fmt.Fprintf(b, "max_depth = %d   # nested scan depth; default 3, set 1 for legacy root-only discovery\n", c.SkillMaxDepth())
	} else {
		b.WriteString("# max_depth = 3   # nested scan depth; set 1 for legacy root-only discovery\n")
	}
	if disabled := c.DisabledSkillNames(); len(disabled) > 0 {
		fmt.Fprintf(b, "disabled_skills = %s   # hidden from the prompt, slash invocation, and skill tools\n\n", renderStringArray(disabled))
	} else {
		b.WriteString("# disabled_skills = [\"review\"]   # hide noisy or unwanted skills\n\n")
	}
}

// renderMemoryConfig writes the [memory] section. Every axis is off or on a
// default by design, so an untouched config renders as comments only.
func renderMemoryConfig(b *strings.Builder, m MemoryConfig) {
	b.WriteString("[memory]\n")
	if m.PinnedBudgetChars > 0 {
		fmt.Fprintf(b, "pinned_budget_chars = %d   # ceiling on total pinned-body characters in the cached prefix\n", m.PinnedBudgetChars)
	} else {
		b.WriteString("# pinned_budget_chars = 1500   # cap pinned guidance; unset = no ceiling, /memory reports the cost\n")
	}
	if m.RecallLimit > 0 {
		fmt.Fprintf(b, "recall_limit = %d   # facts automatic recall may inject per turn\n", m.RecallLimit)
	} else {
		b.WriteString("# recall_limit = 4   # facts automatic recall may inject per turn\n")
	}
	if m.RecallMaxChars > 0 {
		fmt.Fprintf(b, "recall_max_chars = %d   # total size of that injection\n\n", m.RecallMaxChars)
	} else {
		b.WriteString("# recall_max_chars = 2400   # total size of that injection\n\n")
	}
}
