package subagentdef

import (
	"strings"
	"time"
)

// SubagentDefinition describes a subagent configuration loaded from a
// Markdown file with YAML frontmatter. It defines the subagent's name,
// description, allowed tools, model settings, and other behavior parameters.
// Fields marked with yaml:"-" are runtime metadata not persisted in the
// definition file.
type SubagentDefinition struct {
	Name            string    `yaml:"name"`
	Description     string    `yaml:"description"`
	Prompt          string    `yaml:"-"`
	Tools           []string  `yaml:"tools"`
	DisallowedTools []string  `yaml:"disallowed_tools"`
	Model           string    `yaml:"model"`
	Effort          string    `yaml:"effort"`
	PermissionMode  string    `yaml:"permission_mode"`
	MaxTurns        int       `yaml:"max_turns"`
	Skills          []string  `yaml:"skills"`
	MCPServers      []string  `yaml:"mcp_servers"`
	Background      bool      `yaml:"background"`
	Isolation       string    `yaml:"isolation"`
	Color           string    `yaml:"color"`
	Memory          string    `yaml:"memory"`
	InitialPrompt   string    `yaml:"initial_prompt"`
	SourceFile      string    `yaml:"-"`
	SourceScope     string    `yaml:"-"`
	CreatedAt       time.Time `yaml:"-"`
	UpdatedAt       time.Time `yaml:"-"`
}

// Normalize trims whitespace from all string fields and removes empty
// entries from slice fields (tools, disallowed tools, skills, mcp servers).
// Call this after loading a definition to ensure clean data.
func (d *SubagentDefinition) Normalize() {
	d.Name = strings.TrimSpace(d.Name)
	d.Description = strings.TrimSpace(d.Description)
	d.Model = strings.TrimSpace(d.Model)
	d.Effort = strings.TrimSpace(d.Effort)
	d.PermissionMode = strings.TrimSpace(d.PermissionMode)
	d.Isolation = strings.TrimSpace(d.Isolation)
	d.Color = strings.TrimSpace(d.Color)
	d.Memory = strings.TrimSpace(d.Memory)
	d.InitialPrompt = strings.TrimSpace(d.InitialPrompt)

	tools := make([]string, 0, len(d.Tools))
	for _, t := range d.Tools {
		t = strings.TrimSpace(t)
		if t != "" {
			tools = append(tools, t)
		}
	}
	d.Tools = tools

	disallowed := make([]string, 0, len(d.DisallowedTools))
	for _, t := range d.DisallowedTools {
		t = strings.TrimSpace(t)
		if t != "" {
			disallowed = append(disallowed, t)
		}
	}
	d.DisallowedTools = disallowed

	skills := make([]string, 0, len(d.Skills))
	for _, s := range d.Skills {
		s = strings.TrimSpace(s)
		if s != "" {
			skills = append(skills, s)
		}
	}
	d.Skills = skills

	mcpServers := make([]string, 0, len(d.MCPServers))
	for _, s := range d.MCPServers {
		s = strings.TrimSpace(s)
		if s != "" {
			mcpServers = append(mcpServers, s)
		}
	}
	d.MCPServers = mcpServers
}

// Valid reports whether the definition has a non-empty name, which is the
// minimum required field for a usable subagent definition.
func (d *SubagentDefinition) Valid() bool {
	return d.Name != ""
}

// ToolAllowed reports whether a given tool name is permitted for this
// subagent. The check is case-insensitive. If the Tools list is empty,
// all tools are allowed unless explicitly disallowed. DisallowedTools
// takes precedence over the allowlist.
func (d *SubagentDefinition) ToolAllowed(toolName string) bool {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return false
	}
	for _, t := range d.DisallowedTools {
		if strings.EqualFold(t, toolName) {
			return false
		}
	}
	if len(d.Tools) == 0 {
		return true
	}
	for _, t := range d.Tools {
		if strings.EqualFold(t, toolName) {
			return true
		}
	}
	return false
}
