// Package builtinmcp defines MCP servers that ship with Reasonix without
// requiring user configuration.
package builtinmcp

import (
	"os"
	"os/exec"

	"reasonix/internal/config"
)

const (
	TimeName     = "time"
	Context7Name = "context7"
)

var (
	executablePathDefault = os.Executable
	lookPathDefault       = exec.LookPath
	currentExecutable     = executablePathDefault
	lookPath              = lookPathDefault
)

// Entries returns the built-in MCP servers that are always available. They use
// the lazy tier so startup never blocks on package installation or network.
func Entries() []config.PluginEntry {
	return []config.PluginEntry{
		{
			Name:    TimeName,
			Type:    "stdio",
			Command: executablePath(),
			Args:    []string{"builtin-mcp", TimeName},
			Tier:    "lazy",
		},
		context7Entry(),
	}
}

func executablePath() string {
	if path, err := currentExecutable(); err == nil && path != "" {
		return path
	}
	return "reasonix"
}

func context7Entry() config.PluginEntry {
	command, args := context7Command()
	return config.PluginEntry{
		Name:    Context7Name,
		Type:    "stdio",
		Command: command,
		Args:    args,
		Tier:    "lazy",
	}
}

func context7Command() (string, []string) {
	if _, err := lookPath("npx"); err == nil {
		return "npx", []string{"-y", "@upstash/context7-mcp"}
	}
	if _, err := lookPath("pnpm"); err == nil {
		return "pnpm", []string{"dlx", "@upstash/context7-mcp"}
	}
	if _, err := lookPath("bunx"); err == nil {
		return "bunx", []string{"@upstash/context7-mcp"}
	}
	return "npx", []string{"-y", "@upstash/context7-mcp"}
}

// Entry returns one built-in MCP entry by name.
func Entry(name string) (config.PluginEntry, bool) {
	for _, e := range Entries() {
		if e.Name == name {
			return e, true
		}
	}
	return config.PluginEntry{}, false
}

// IsBuiltIn reports whether name is a Reasonix-shipped MCP server.
func IsBuiltIn(name string) bool {
	_, ok := Entry(name)
	return ok
}

// AppendMissing appends built-in MCP entries unless a configured or
// session-scoped entry with the same name exists. Explicit user and host config
// wins, including auto_start=false.
func AppendMissing(out []config.PluginEntry, configured []config.PluginEntry, reservedNames ...string) []config.PluginEntry {
	return AppendEnabled(out, configured, []string{TimeName, Context7Name}, reservedNames...)
}

// AppendEnabled is like AppendMissing but only appends enabled built-in names.
func AppendEnabled(out []config.PluginEntry, configured []config.PluginEntry, enabledNames []string, reservedNames ...string) []config.PluginEntry {
	seen := make(map[string]bool, len(configured))
	for _, e := range configured {
		seen[e.Name] = true
	}
	for _, name := range reservedNames {
		seen[name] = true
	}
	enabled := make(map[string]bool, len(enabledNames))
	for _, name := range enabledNames {
		enabled[name] = true
	}
	for _, e := range Entries() {
		if enabled[e.Name] && !seen[e.Name] {
			out = append(out, e)
		}
	}
	return out
}
