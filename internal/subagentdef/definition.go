package subagentdef

import (
	"strings"
	"time"
)

type SubagentDefinition struct {
	Name             string        `yaml:"name"`
	Description      string        `yaml:"description"`
	Prompt           string        `yaml:"-"`
	Tools            []string      `yaml:"tools"`
	DisallowedTools  []string      `yaml:"disallowed_tools"`
	Model            string        `yaml:"model"`
	Effort           string        `yaml:"effort"`
	PermissionMode   string        `yaml:"permission_mode"`
	MaxTurns         int           `yaml:"max_turns"`
	Skills           []string      `yaml:"skills"`
	MCPServers       []string      `yaml:"mcp_servers"`
	Background       bool          `yaml:"background"`
	Isolation        string        `yaml:"isolation"`
	Color            string        `yaml:"color"`
	Memory           string        `yaml:"memory"`
	InitialPrompt    string        `yaml:"initial_prompt"`
	SourceFile       string        `yaml:"-"`
	SourceScope      string        `yaml:"-"`
	CreatedAt        time.Time     `yaml:"-"`
	UpdatedAt        time.Time     `yaml:"-"`
}

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

func (d *SubagentDefinition) Valid() bool {
	return d.Name != ""
}

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
