package config

import (
	"fmt"
	"path/filepath"
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
			if !stringSliceContainsPath(cfg.Skills.Paths, skillRoot) {
				cfg.Skills.Paths = append(cfg.Skills.Paths, skillRoot)
			}
			if cfg.pluginPackageSkillOwners == nil {
				cfg.pluginPackageSkillOwners = map[string][]string{}
			}
			key := CanonicalSkillPath(skillRoot)
			if !containsString(cfg.pluginPackageSkillOwners[key], item.Installed.Name) {
				cfg.pluginPackageSkillOwners[key] = append(cfg.pluginPackageSkillOwners[key], item.Installed.Name)
			}
		}
		for name, srv := range pkg.Manifest.MCPServers {
			if pluginNameExists(cfg.Plugins, name) {
				warnings = append(warnings, fmt.Sprintf("%s: plugin MCP server %q skipped because config already defines that name", item.Installed.Name, name))
				continue
			}
			entry := PluginEntry{
				Name:      name,
				Type:      srv.Type,
				Command:   pluginPackageCommand(pkg.Root, srv.Command),
				Args:      append([]string(nil), srv.Args...),
				Env:       pluginPackageEnv(item.Installed, pkg.Root, srv.Env),
				URL:       strings.TrimSpace(srv.URL),
				Headers:   cloneStringMap(srv.Headers),
				AutoStart: srv.AutoStart,
				Tier:      srv.Tier,
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
	command = strings.TrimSpace(command)
	if command == "" || filepath.IsAbs(command) {
		return command
	}
	return filepath.Join(root, filepath.FromSlash(command))
}

func pluginPackageEnv(installed pluginpkg.InstalledPlugin, root string, env map[string]string) map[string]string {
	out := cloneStringMap(env)
	if out == nil {
		out = map[string]string{}
	}
	out["REASONIX_PLUGIN_ROOT"] = root
	out["REASONIX_PLUGIN_NAME"] = installed.Name
	if installed.Version != "" {
		out["REASONIX_PLUGIN_VERSION"] = installed.Version
	}
	return out
}

func pluginNameExists(entries []PluginEntry, name string) bool {
	for _, p := range entries {
		if p.Name == name {
			return true
		}
	}
	return false
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
