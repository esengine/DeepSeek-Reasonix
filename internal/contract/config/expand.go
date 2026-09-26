package config

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// varRef matches ${VAR} and ${VAR:-default}: a shell-style reference with an
// optional ":-default" fallback used when the variable is unset or empty.
var varRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

type envLookup func(string) (string, bool)

// ExpandVars substitutes ${VAR} / ${VAR:-default} references from the process
// environment. An unset variable with no default expands to "" (matching the
// MCP / Claude Code convention), so a missing secret yields an empty header
// rather than a literal "${TOKEN}" leaking onto the wire.
func ExpandVars(s string) string {
	return expandVarsWithLookup(s, func(name string) (string, bool) {
		v, ok := os.LookupEnv(name)
		return v, ok && v != ""
	})
}

func expandVarsWithLookup(s string, lookup envLookup) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return varRef.ReplaceAllStringFunc(s, func(m string) string {
		g := varRef.FindStringSubmatch(m)
		name, hasDefault, def := g[1], g[2] != "", g[3]
		if v, ok := lookup(name); ok {
			return v
		}
		if hasDefault {
			return def
		}
		return ""
	})
}

func scopedEnvLookup(scoped map[string]string) envLookup {
	return func(name string) (string, bool) {
		if v, ok := os.LookupEnv(name); ok {
			return v, v != ""
		}
		if v, ok := scoped[name]; ok && v != "" {
			return v, true
		}
		return "", false
	}
}

func (c *Config) expandVars(s string) string {
	if c == nil {
		return ExpandVars(s)
	}
	return expandVarsWithLookup(s, scopedEnvLookup(c.expansionEnv))
}

// workspaceRootVars name the workspace a server runs for. The host owns their
// value, so it outranks a process variable of the same name: a hook's child
// process inherits its parent session's CLAUDE_PROJECT_DIR.
var workspaceRootVars = []string{"REASONIX_WORKSPACE_ROOT", "CLAUDE_PROJECT_DIR"}

// WorkspaceRootValue is the form of root a plugin entry sees for the workspace
// variables: absolute and cleaned, symlinks left as the workspace was opened.
func WorkspaceRootValue(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	return abs
}

func workspaceEnvLookup(scoped map[string]string, root string) envLookup {
	base := scopedEnvLookup(scoped)
	root = WorkspaceRootValue(root)
	if root == "" {
		return base
	}
	return func(name string) (string, bool) {
		if slices.Contains(workspaceRootVars, name) {
			return root, true
		}
		return base(name)
	}
}

// ExpandedPlugin is ExpandedPluginForRoot with no workspace.
func (e PluginEntry) ExpandedPlugin() PluginEntry { return e.ExpandedPluginForRoot("") }

// ExpandedPluginForRoot returns a copy of e with ${VAR} references expanded
// across command, args, env values, url, and header values. With a root,
// ${REASONIX_WORKSPACE_ROOT} and ${CLAUDE_PROJECT_DIR} name that workspace.
func (e PluginEntry) ExpandedPluginForRoot(root string) PluginEntry {
	lookup := workspaceEnvLookup(e.expansionEnv, root)
	out := e
	out.Command = expandVarsWithLookup(e.Command, lookup)
	out.URL = expandVarsWithLookup(e.URL, lookup)
	if len(e.Args) > 0 {
		out.Args = make([]string, len(e.Args))
		for i, a := range e.Args {
			out.Args[i] = expandVarsWithLookup(a, lookup)
		}
	}
	out.Env = expandMap(e.Env, lookup)
	out.Headers = expandMap(e.Headers, lookup)
	return out
}

func expandMap(m map[string]string, lookup envLookup) map[string]string {
	if len(m) == 0 {
		return m
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = expandVarsWithLookup(v, lookup)
	}
	return out
}
