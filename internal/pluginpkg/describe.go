package pluginpkg

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// InstalledNames returns installed plugin package names sorted for completion
// menus and lightweight management views.
func InstalledNames(reasonixHome string) ([]string, error) {
	st, err := LoadState(reasonixHome)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(st.Plugins))
	for _, p := range st.Plugins {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names, nil
}

// InstalledListText returns a compact session-facing view of installed plugins.
func InstalledListText(reasonixHome string) (string, error) {
	st, err := LoadState(reasonixHome)
	if err != nil {
		return "", err
	}
	if len(st.Plugins) == 0 {
		return "plugins: none installed\ninstall: reasonix plugin install <source> --yes, or use Settings -> Plugins", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "plugins (%d):\n", len(st.Plugins))
	for _, p := range st.Plugins {
		state := "disabled"
		if p.Enabled {
			state = "enabled"
		}
		summary := p.Description
		counts := pluginCapabilityText(reasonixHome, p)
		if summary != "" && counts != "" {
			summary = counts + " - " + oneLine(summary)
		} else if counts != "" {
			summary = counts
		} else if summary != "" {
			summary = oneLine(summary)
		}
		if summary != "" {
			fmt.Fprintf(&b, "  %s [%s] - %s\n", p.Name, state, summary)
		} else {
			fmt.Fprintf(&b, "  %s [%s]\n", p.Name, state)
		}
	}
	b.WriteString("show details: /plugins show <name>")
	return strings.TrimRight(b.String(), "\n"), nil
}

// InstalledShowText returns the usage-oriented details for one installed plugin.
func InstalledShowText(reasonixHome, name string) (string, error) {
	p, ok, err := FindInstalled(reasonixHome, name)
	if err != nil {
		return "", err
	}
	if !ok {
		return fmt.Sprintf("plugin %q is not installed", name), nil
	}
	root := ResolveRoot(reasonixHome, p.Root)
	pkg, warnings, err := ParseDir(root)
	if err != nil {
		return "", err
	}
	skills, hooks, mcp := pkg.CapabilityCounts()
	state := "disabled"
	if p.Enabled {
		state = "enabled"
	}
	version := p.Version
	if version == "" {
		version = "-"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "plugin %s [%s]\n", p.Name, state)
	fmt.Fprintf(&b, "version: %s\nkind: %s\nroot: %s\nsource: %s\ncapabilities: %d skills, %d hooks, %d MCP servers\n", version, p.ManifestKind, filepath.Clean(root), p.Source, skills, hooks, mcp)
	if p.Enabled {
		b.WriteString("usage: enabled plugins load into new sessions; use /skills, invoke /<skill>, or ask naturally.\n")
	} else {
		b.WriteString("usage: enable this plugin before its skills, hooks, or MCP servers participate in sessions.\n")
	}
	appendInventoryText(&b, pkg.Inventory())
	for _, warning := range warnings {
		fmt.Fprintf(&b, "warning: %s\n", warning)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func FindInstalled(reasonixHome, name string) (InstalledPlugin, bool, error) {
	st, err := LoadState(reasonixHome)
	if err != nil {
		return InstalledPlugin{}, false, err
	}
	for _, p := range st.Plugins {
		if p.Name == name {
			return p, true, nil
		}
	}
	return InstalledPlugin{}, false, nil
}

func pluginCapabilityText(reasonixHome string, p InstalledPlugin) string {
	root := ResolveRoot(reasonixHome, p.Root)
	pkg, _, err := ParseDir(root)
	if err != nil {
		return "invalid: " + err.Error()
	}
	skills, hooks, mcp := pkg.CapabilityCounts()
	parts := []string{}
	if skills > 0 {
		parts = append(parts, fmt.Sprintf("%d skills", skills))
	}
	if hooks > 0 {
		parts = append(parts, fmt.Sprintf("%d hooks", hooks))
	}
	if mcp > 0 {
		parts = append(parts, fmt.Sprintf("%d MCP", mcp))
	}
	if len(parts) == 0 {
		return "no exported capabilities"
	}
	return strings.Join(parts, " / ")
}

func appendInventoryText(b *strings.Builder, inv Inventory) {
	if len(inv.Skills) > 0 {
		b.WriteString("skills:\n")
		for _, sk := range inv.Skills {
			desc := oneLine(sk.Description)
			if desc == "" {
				desc = "(no description)"
			}
			invocation := sk.Invocation
			if invocation == "" {
				invocation = "/" + sk.Name
			}
			if sk.RunAs != "" {
				fmt.Fprintf(b, "  %s [%s] - %s\n", invocation, sk.RunAs, desc)
			} else {
				fmt.Fprintf(b, "  %s - %s\n", invocation, desc)
			}
		}
	}
	if len(inv.Hooks) > 0 {
		b.WriteString("hooks:\n")
		for _, hook := range inv.Hooks {
			target := hook.Command
			if target == "" {
				target = hook.ContextFile
			}
			match := hook.Match
			if match == "" {
				match = "*"
			}
			desc := oneLine(hook.Description)
			if desc != "" {
				fmt.Fprintf(b, "  %s match=%s - %s - %s\n", hook.Event, match, target, desc)
			} else {
				fmt.Fprintf(b, "  %s match=%s - %s\n", hook.Event, match, target)
			}
		}
	}
	if len(inv.MCPServers) > 0 {
		b.WriteString("mcpServers:\n")
		for _, server := range inv.MCPServers {
			target := server.Command
			if target == "" {
				target = server.URL
			}
			fmt.Fprintf(b, "  %s [%s] - %s\n", server.Name, server.Transport, target)
		}
	}
	if len(inv.Skills) == 0 && len(inv.Hooks) == 0 && len(inv.MCPServers) == 0 {
		b.WriteString("capabilities: no detailed inventory available\n")
	}
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
