package config

import (
	"os"
	"regexp"
	"strings"
)

// varRef matches ${VAR} and ${VAR:-default}: a shell-style reference with an
// optional ":-default" fallback used when the variable is unset or empty.
var varRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// ExpandVars substitutes ${VAR} / ${VAR:-default} references from the process
// environment. An unset variable with no default expands to "" (matching the
// MCP / Claude Code convention), so a missing secret yields an empty header
// rather than a literal "${TOKEN}" leaking onto the wire.
func ExpandVars(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return varRef.ReplaceAllStringFunc(s, func(m string) string {
		g := varRef.FindStringSubmatch(m)
		name, hasDefault, def := g[1], g[2] != "", g[3]
		if v, ok := os.LookupEnv(name); ok && v != "" {
			return v
		}
		if hasDefault {
			return def
		}
		return ""
	})
}

// ExpandedPlugin returns a copy of e with ${VAR} references expanded across the
// command, args, env values, url, and header values — the fields Claude Code
// also expands. The entry itself is left untouched.
func (e PluginEntry) ExpandedPlugin() PluginEntry {
	out := e
	out.Command = ExpandVars(e.Command)
	out.URL = ExpandVars(e.URL)
	if len(e.Args) > 0 {
		out.Args = make([]string, len(e.Args))
		for i, a := range e.Args {
			out.Args[i] = ExpandVars(a)
		}
	}
	out.Env = expandMap(e.Env)
	out.Headers = expandMap(e.Headers)
	return out
}

func expandMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return m
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = ExpandVars(v)
	}
	return out
}
