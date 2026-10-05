package config

import (
	"fmt"
	"strings"
)

// renderCLIConfig writes the [cli] section, which is user-global only: a
// repo-local reasonix.toml must never run an external diff formatter, and the
// diff-fence toggle is user/global for the same reason. The section is emitted
// only when set, so the retired update_channel key never reappears.
func renderCLIConfig(b *strings.Builder, c *Config, scope RenderScope) {
	if scope == RenderScopeProject {
		return
	}
	cmd := strings.TrimSpace(c.CLI.DiffFormatter)
	if cmd == "" && !c.CLI.DiffFences {
		return
	}
	b.WriteString("[cli]\n")
	if cmd != "" {
		fmt.Fprintf(b, "diff_formatter = %q   # external argv (no shell) that formats a fenced diff block, a writer diff card, and a whole-diff shell result; the diff is piped on stdin and its stdout re-emitted with non-SGR escapes stripped; user-global only\n", cmd)
	}
	if c.CLI.DiffFences {
		b.WriteString("diff_fences = true   # render a fenced ```diff/```patch block through the colourised diff renderer instead of the plain code rail; default false; user-global only\n")
	}
	b.WriteString("\n")
}
