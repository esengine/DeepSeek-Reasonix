package config

import (
	"fmt"
	"strings"
)

// MCP load modes. A deferred server's tools are reached through
// use_capability; an always-loaded one's tools are in the provider schema.
const (
	MCPLoadDeferred = "deferred"
	MCPLoadAlways   = "always"
)

func normalizeMCPLoad(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", MCPLoadDeferred:
		return MCPLoadDeferred, true
	case MCPLoadAlways:
		return MCPLoadAlways, true
	default:
		return MCPLoadDeferred, false
	}
}

// DeclaredMCPLoad is the mode the server's own declaration asks for. An
// unknown value is deferred, the mode that costs nothing when it is wrong.
func (e PluginEntry) DeclaredMCPLoad() string {
	mode, _ := normalizeMCPLoad(e.Load)
	return mode
}

// MCPLoadOverride reports the user's [tools.mcp_load] choice for name.
func (c *Config) MCPLoadOverride(name string) (string, bool) {
	if c == nil {
		return "", false
	}
	raw, ok := c.Tools.MCPLoad[strings.TrimSpace(name)]
	if !ok {
		return "", false
	}
	mode, _ := normalizeMCPLoad(raw)
	return mode, true
}

// MCPAlwaysLoad resolves whether entry's tools belong in the provider schema:
// the user's override first, then the server's declaration.
func (c *Config) MCPAlwaysLoad(entry PluginEntry) bool {
	if mode, ok := c.MCPLoadOverride(entry.Name); ok {
		return mode == MCPLoadAlways
	}
	return entry.DeclaredMCPLoad() == MCPLoadAlways
}

// SetMCPLoad records the user's load mode for a server. An empty mode clears
// the override so the server's declaration applies again.
func (c *Config) SetMCPLoad(name, mode string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("mcp load: server name is required")
	}
	if strings.TrimSpace(mode) == "" {
		delete(c.Tools.MCPLoad, name)
		if len(c.Tools.MCPLoad) == 0 {
			c.Tools.MCPLoad = nil
		}
		return nil
	}
	normalized, ok := normalizeMCPLoad(mode)
	if !ok {
		return fmt.Errorf("mcp load %q: want %q or %q", mode, MCPLoadAlways, MCPLoadDeferred)
	}
	if c.Tools.MCPLoad == nil {
		c.Tools.MCPLoad = map[string]string{}
	}
	c.Tools.MCPLoad[name] = normalized
	return nil
}

// SetUserMCPLoad records a server's load mode in the user config. It reaches
// the provider schema when a runtime is next assembled.
func SetUserMCPLoad(name, mode string) error {
	return EditConfigFile(UserConfigPath(), func(cfg *Config) error {
		return cfg.SetMCPLoad(name, mode)
	})
}
