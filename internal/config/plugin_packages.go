package config

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"reasonix/internal/command"
	"reasonix/internal/pluginpkg"
)

// mergeInstalledPluginPackages overlays enabled plugin package capabilities onto
// the in-memory config. It never writes config.toml: plugin package state lives
// in <Reasonix home>/plugin-packages.json so uninstall/disable can remove the
// entire bundle without editing user-authored config.
func mergeInstalledPluginPackages(cfg *Config, root string) []string {
	if cfg == nil {
		return nil
	}
	reasonixHome := ReasonixHomeDir()
	if strings.TrimSpace(reasonixHome) == "" {
		return nil
	}
	installed, warnings := pluginpkg.LoadInstalled(reasonixHome)
	sort.SliceStable(installed, func(i, j int) bool {
		return installed[i].Installed.Name < installed[j].Installed.Name
	})
	for _, item := range installed {
		pkg := item.Package
		for _, warning := range item.Warnings {
			warnings = append(warnings, fmt.Sprintf("%s: %s", item.Installed.Name, warning))
		}
		for _, skillRoot := range pkg.SkillRoots() {
			cfg.addPluginSkillRoot(skillRoot, item.Installed.Name, false)
		}
		for _, agentRoot := range pkg.AgentRoots() {
			cfg.addPluginSkillRoot(agentRoot, item.Installed.Name, true)
		}
		for name, srv := range pkg.Manifest.MCPServers {
			entry := PluginEntry{
				Name:      name,
				Type:      srv.Type,
				Command:   pluginPackageCommand(pkg.Root, pluginPackageWorkspaceValue(pkg.Root, root, srv.Command)),
				Args:      pluginPackageWorkspaceValues(pkg.Root, root, srv.Args),
				Env:       pluginPackageEnv(item.Installed, pkg.Root, root, srv.Env),
				URL:       pluginPackageWorkspaceValue(pkg.Root, root, strings.TrimSpace(srv.URL)),
				Headers:   pluginPackageWorkspaceMap(pkg.Root, root, srv.Headers),
				AutoStart: srv.AutoStart,
				Tier:      srv.Tier,
				Source:    MCPSourcePluginPackage,
			}
			if existing, ok := pluginEntryByName(cfg.Plugins, name); ok {
				if owner, packageOwned := cfg.pluginPackageOwners[name]; packageOwned && pluginPackageEntriesEqual(existing, entry) {
					continue
				} else if packageOwned {
					warnings = append(warnings, fmt.Sprintf("%s: plugin MCP server %q conflicts with package %s and was skipped", item.Installed.Name, name, owner))
				} else {
					warnings = append(warnings, fmt.Sprintf("%s: plugin MCP server %q skipped because config already defines that name", item.Installed.Name, name))
				}
				continue
			}
			cfg.Plugins = append(cfg.Plugins, entry)
			if cfg.pluginPackageOwners == nil {
				cfg.pluginPackageOwners = map[string]string{}
			}
			cfg.pluginPackageOwners[name] = item.Installed.Name
		}
	}
	return warnings
}

func (c *Config) addPluginSkillRoot(root, plugin string, agent bool) {
	if !stringSliceContainsPath(c.Skills.Paths, root) {
		c.Skills.Paths = append(c.Skills.Paths, root)
	}
	if c.pluginPackageSkillOwners == nil {
		c.pluginPackageSkillOwners = map[string][]string{}
	}
	key := CanonicalSkillPath(root)
	if !containsString(c.pluginPackageSkillOwners[key], plugin) {
		c.pluginPackageSkillOwners[key] = append(c.pluginPackageSkillOwners[key], plugin)
	}
	if !agent {
		return
	}
	if c.pluginPackageAgentOwners == nil {
		c.pluginPackageAgentOwners = map[string][]string{}
	}
	if !containsString(c.pluginPackageAgentOwners[key], plugin) {
		c.pluginPackageAgentOwners[key] = append(c.pluginPackageAgentOwners[key], plugin)
	}
}

// PluginPackageSkillOwners returns installed plugin package names keyed by
// canonical skill-root path. Multiple linked installs may intentionally point
// at the same root under different package names.
func (c *Config) PluginPackageSkillOwners() map[string][]string {
	if c == nil || len(c.pluginPackageSkillOwners) == 0 {
		return nil
	}
	out := make(map[string][]string, len(c.pluginPackageSkillOwners))
	for path, owners := range c.pluginPackageSkillOwners {
		out[path] = append([]string(nil), owners...)
	}
	return out
}

// PluginPackageAgentOwners identifies Claude agents/ roots that must be loaded
// as manually invoked subagent profiles rather than ordinary inline skills.
func (c *Config) PluginPackageAgentOwners() map[string][]string {
	if c == nil || len(c.pluginPackageAgentOwners) == 0 {
		return nil
	}
	out := make(map[string][]string, len(c.pluginPackageAgentOwners))
	for path, owners := range c.pluginPackageAgentOwners {
		out[path] = append([]string(nil), owners...)
	}
	return out
}

// pluginPackageCommandRoots returns the command directories contributed by
// enabled installed plugin packages, in deterministic (name, path) order.
// CommandRootsForRoot places them ahead of every user/project dir so explicit
// commands win exact canonical-name clashes; LoadInstalled filters to enabled
// packages.
func pluginPackageCommandRoots() []command.Root {
	reasonixHome := ReasonixHomeDir()
	if strings.TrimSpace(reasonixHome) == "" {
		return nil
	}
	installed, _ := pluginpkg.LoadInstalled(reasonixHome)
	var out []command.Root
	for _, item := range installed {
		for _, root := range item.Package.CommandRoots() {
			out = append(out, command.Root{Path: root, Plugin: item.Installed.Name})
		}
	}
	return out
}

// PluginPackageOwner reports the installed plugin package that contributed an
// MCP server. Config-authored servers with the same name win during merge and
// therefore have no package owner.
func (c *Config) PluginPackageOwner(name string) (string, bool) {
	if c == nil || len(c.pluginPackageOwners) == 0 {
		return "", false
	}
	owner, ok := c.pluginPackageOwners[strings.TrimSpace(name)]
	return owner, ok
}

func pluginPackageCommand(root, command string) string {
	command = pluginPackageValue(root, strings.TrimSpace(command))
	if command == "" || filepath.IsAbs(command) {
		return command
	}
	return filepath.Join(root, filepath.FromSlash(command))
}

func pluginPackageEnv(installed pluginpkg.InstalledPlugin, root, workspaceRoot string, env map[string]string) map[string]string {
	out := pluginPackageWorkspaceMap(root, workspaceRoot, env)
	if out == nil {
		out = map[string]string{}
	}
	out["REASONIX_PLUGIN_ROOT"] = root
	out["REASONIX_PLUGIN_NAME"] = installed.Name
	out["CLAUDE_PLUGIN_ROOT"] = root
	out["CLAUDE_PROJECT_DIR"] = workspaceRoot
	out["REASONIX_WORKSPACE_ROOT"] = workspaceRoot
	if installed.Version != "" {
		out["REASONIX_PLUGIN_VERSION"] = installed.Version
	}
	return out
}

func pluginPackageWorkspaceValue(root, workspaceRoot, value string) string {
	value = pluginPackageValue(root, value)
	value = expandPluginPathVar(value, "${CLAUDE_PROJECT_DIR}", workspaceRoot)
	return expandPluginPathVar(value, "$CLAUDE_PROJECT_DIR", workspaceRoot)
}

func pluginPackageWorkspaceValues(root, workspaceRoot string, values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = pluginPackageWorkspaceValue(root, workspaceRoot, value)
	}
	return out
}

func pluginPackageWorkspaceMap(root, workspaceRoot string, values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = pluginPackageWorkspaceValue(root, workspaceRoot, value)
	}
	return out
}

func pluginPackageValue(root, value string) string {
	value = expandPluginPathVar(value, "${CLAUDE_PLUGIN_ROOT}", root)
	return expandPluginPathVar(value, "$CLAUDE_PLUGIN_ROOT", root)
}

// expandPluginPathVar replaces every occurrence of placeholder with root and
// normalizes the path suffix that follows each occurrence (up to the next
// "$" or the end of the string) to the host separator. Claude manifests
// always author that suffix with "/" regardless of host OS (e.g.
// "${CLAUDE_PLUGIN_ROOT}/bin/server"); root is already OS-native, so a plain
// string replace leaves a mixed "C:\...\pkg/bin/server" value on Windows that
// no longer round-trips through filepath.Join comparisons.
func expandPluginPathVar(value, placeholder, root string) string {
	var b strings.Builder
	rest := value
	for {
		idx := strings.Index(rest, placeholder)
		if idx < 0 {
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(rest[:idx])
		b.WriteString(root)
		rest = rest[idx+len(placeholder):]
		end := strings.IndexByte(rest, '$')
		suffix := rest
		if end >= 0 {
			suffix = rest[:end]
		}
		b.WriteString(filepath.FromSlash(suffix))
		if end < 0 {
			return b.String()
		}
		rest = rest[end:]
	}
}

func pluginEntryByName(entries []PluginEntry, name string) (PluginEntry, bool) {
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return PluginEntry{}, false
}

func pluginPackageEntriesEqual(a, b PluginEntry) bool {
	a.Env = cloneStringMap(a.Env)
	b.Env = cloneStringMap(b.Env)
	for _, env := range []map[string]string{a.Env, b.Env} {
		delete(env, "REASONIX_PLUGIN_ROOT")
		delete(env, "REASONIX_PLUGIN_NAME")
		delete(env, "REASONIX_PLUGIN_VERSION")
		delete(env, "CLAUDE_PLUGIN_ROOT")
	}
	return reflect.DeepEqual(a, b)
}

func stringSliceContainsPath(paths []string, path string) bool {
	canon := CanonicalSkillPath(path)
	for _, existing := range paths {
		if CanonicalSkillPath(ExpandVars(existing)) == canon {
			return true
		}
	}
	return false
}
