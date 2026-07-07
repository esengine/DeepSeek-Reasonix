package config

import (
	"path/filepath"
	"strings"
	"unicode"

	"reasonix/internal/shellparse"
)

// NormalizePluginCommandLine repairs the common MCP copy/paste mistake where a
// tutorial's full command line is placed in command while args is left empty.
// Valid commands that are just paths with spaces are left untouched when they
// look path-like; ordinary custom commands such as "custom-mcp --stdio" still
// split so the executable and arguments survive GUI/legacy normalization.
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
	return knownMCPCommandRunner(first) || !hasPathSeparator(first)
}

func hasPathSeparator(s string) bool {
	return strings.ContainsAny(s, `/\`)
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
	if preservesUnquotedWindowsPath(s) {
		return nil, false
	}
	fields, malformed := shellparse.StaticFields(s)
	if malformed != "" {
		return nil, false
	}
	return fields, true
}

func preservesUnquotedWindowsPath(s string) bool {
	trimmed := strings.TrimLeftFunc(s, unicode.IsSpace)
	if strings.HasPrefix(trimmed, `"`) || strings.HasPrefix(trimmed, `'`) {
		return false
	}
	first, _, _ := strings.Cut(trimmed, " ")
	first, _, _ = strings.Cut(first, "\t")
	return strings.Contains(first, `\`) && strings.ContainsAny(trimmed, " \t\r\n")
}
