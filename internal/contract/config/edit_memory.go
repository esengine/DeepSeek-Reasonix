package config

import (
	"fmt"
	"strings"
)

// SetMemoryProactive sets whether the model asks before remembering, writes on
// its own, or is not offered the remember tool.
func (c *Config) SetMemoryProactive(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "ask", "":
		c.Memory.Proactive = "ask"
	case "auto":
		c.Memory.Proactive = "auto"
	case "off":
		c.Memory.Proactive = "off"
	default:
		return fmt.Errorf("memory.proactive %q: must be ask|auto|off", mode)
	}
	return nil
}
