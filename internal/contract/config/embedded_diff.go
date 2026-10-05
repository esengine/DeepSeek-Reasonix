package config

import (
	"fmt"
	"strings"
)

// EmbeddedDiffDetectionEnabled reports whether the host marks a shell tool
// result whose whole output is a unified diff. Default false: the detection is
// opt-in.
func (c *Config) EmbeddedDiffDetectionEnabled() bool {
	return c != nil && c.Agent.EmbeddedDiffDetection != nil && *c.Agent.EmbeddedDiffDetection
}

// renderEmbeddedDiffDetection writes the [agent] toggle, commented when unset.
func renderEmbeddedDiffDetection(b *strings.Builder, c *Config) {
	if c.Agent.EmbeddedDiffDetection != nil {
		fmt.Fprintf(b, "embedded_diff_detection = %v   # mark a shell result that is a whole unified diff so a frontend renders it as a diff\n", *c.Agent.EmbeddedDiffDetection)
		return
	}
	b.WriteString("# embedded_diff_detection = true   # mark a shell result that is a whole unified diff so a frontend renders it as a diff (default false)\n")
}
