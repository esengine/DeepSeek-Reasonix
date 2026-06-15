package config

import (
	"path/filepath"
	"strings"
	"unicode"
)

// NormalizePluginCommandLine repairs the common MCP copy/paste mistake where a
// tutorial's full command line is placed in command while args is left empty.
// Valid commands that are just paths with spaces are left untouched unless they
// are quoted or start with a known MCP runner such as npx/uvx/node.
func NormalizePluginCommandLine(e PluginEntry) (PluginEntry, bool) {
	if pluginEntryTransport(e) != "stdio" || len(e.Args) > 0 {
		e.Command = strings.TrimSpace(e.Command)
		return e, false
	}
	cmd := strings.TrimSpace(e.Command)
	e.Command = cmd
	if !strings.ContainsAny(cmd, " \t\r\n") {
		return e, false
	}
	parts, ok := splitPluginCommandLine(cmd)
	if !ok || len(parts) < 2 || !shouldSplitPluginCommand(cmd, parts[0]) {
		return e, false
	}
	e.Command = parts[0]
	e.Args = parts[1:]
	return e, true
}

func normalizePluginCommandLines(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Plugins {
		c.Plugins[i], _ = NormalizePluginCommandLine(c.Plugins[i])
	}
}

func pluginEntryTransport(e PluginEntry) string {
	switch strings.ToLower(strings.TrimSpace(e.Type)) {
	case "", "stdio":
		return "stdio"
	case "http", "streamable-http":
		return "http"
	case "sse":
		return "sse"
	default:
		return strings.ToLower(strings.TrimSpace(e.Type))
	}
}

func shouldSplitPluginCommand(original, first string) bool {
	trimmed := strings.TrimLeftFunc(original, unicode.IsSpace)
	if strings.HasPrefix(trimmed, `"`) || strings.HasPrefix(trimmed, `'`) {
		return true
	}
	return knownMCPCommandRunner(first)
}

func knownMCPCommandRunner(command string) bool {
	base := commandBase(command)
	base = strings.ToLower(base)
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		base = strings.TrimSuffix(base, ext)
	}
	switch base {
	case "npx", "npm", "node", "pnpm", "yarn", "bun",
		"uvx", "uv", "python", "python3", "py",
		"docker", "deno", "go", "cmd", "powershell", "pwsh":
		return true
	default:
		return false
	}
}

func commandBase(command string) string {
	command = strings.ReplaceAll(command, `\`, `/`)
	return filepath.Base(command)
}

func splitPluginCommandLine(s string) ([]string, bool) {
	var parts []string
	var b strings.Builder
	var quote rune
	inToken := false
	flush := func() {
		if !inToken {
			return
		}
		parts = append(parts, b.String())
		b.Reset()
		inToken = false
	}
	for _, r := range s {
		if quote == 0 && unicode.IsSpace(r) {
			flush()
			continue
		}
		if r == '"' || r == '\'' {
			switch quote {
			case 0:
				quote = r
				inToken = true
				continue
			case r:
				quote = 0
				continue
			}
		}
		inToken = true
		b.WriteRune(r)
	}
	if quote != 0 {
		return nil, false
	}
	flush()
	return parts, true
}
