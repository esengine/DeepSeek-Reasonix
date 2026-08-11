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
	summary := pkg.CapabilitySummary()
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
	fmt.Fprintf(&b, "version: %s\nkind: %s\nroot: %s\nsource: %s\ncapabilities: %d skills, %d commands, %d prompts, %d hooks, %d MCP servers, %d themes\n", version, p.ManifestKind, filepath.Clean(root), p.Source, summary.Skills, summary.Commands, summary.Prompts, summary.Hooks, summary.MCPServers, summary.Themes)
	if summary.Runtime {
		b.WriteString(RuntimeTrustText(pkg.Manifest.Runtime))
	}
	if p.Enabled {
		b.WriteString("usage: enabled plugins load into new sessions; use /skills, invoke /<plugin>:<skill> or /<plugin>:<command>, or ask naturally.\n")
	} else {
		b.WriteString("usage: enable this plugin before its skills, commands, hooks, or MCP servers participate in sessions.\n")
	}
	appendInventoryText(&b, p.Name, pkg.Inventory())
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
	summary := pkg.CapabilitySummary()
	parts := []string{}
	if summary.Skills > 0 {
		parts = append(parts, fmt.Sprintf("%d skills", summary.Skills))
	}
	if summary.Commands > 0 {
		parts = append(parts, fmt.Sprintf("%d commands", summary.Commands))
	}
	if summary.Prompts > 0 {
		parts = append(parts, fmt.Sprintf("%d prompts", summary.Prompts))
	}
	if summary.Hooks > 0 {
		parts = append(parts, fmt.Sprintf("%d hooks", summary.Hooks))
	}
	if summary.MCPServers > 0 {
		parts = append(parts, fmt.Sprintf("%d MCP", summary.MCPServers))
	}
	if summary.Themes > 0 {
		parts = append(parts, fmt.Sprintf("%d themes", summary.Themes))
	}
	if summary.Runtime {
		parts = append(parts, "FULL TRUST runtime")
	}
	if len(parts) == 0 {
		return "no exported capabilities"
	}
	return strings.Join(parts, " / ")
}

// RuntimeTrustText renders the prominent FULL TRUST block for a package that
// declares a runtime process. CLI and session-facing views share it so the
// risk statement reads identically everywhere.
func RuntimeTrustText(rt *RuntimeSpec) string {
	if rt == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("runtime: FULL TRUST\n")
	fmt.Fprintf(&b, "  command: %s\n", RuntimeCommandLine(rt))
	if len(rt.Intercepts) > 0 {
		fmt.Fprintf(&b, "  intercepts: %s\n", strings.Join(rt.Intercepts, ", "))
	}
	if len(rt.Replaces) > 0 {
		fmt.Fprintf(&b, "  replaces: %s\n", strings.Join(rt.Replaces, ", "))
	}
	if len(rt.Capabilities) > 0 {
		fmt.Fprintf(&b, "  capabilities: %s\n", strings.Join(rt.Capabilities, ", "))
	}
	b.WriteString("  risk: the runtime process runs inside Reasonix — it can read the full session and environment, bypass permissions, and operate this machine directly.\n")
	return b.String()
}

// RuntimeCommandLine renders the exec-form runtime command plus args as one
// display line. The command is never executed through a shell.
func RuntimeCommandLine(rt *RuntimeSpec) string {
	if rt == nil {
		return ""
	}
	return strings.Join(append([]string{rt.Command}, rt.Args...), " ")
}

func appendInventoryText(b *strings.Builder, pluginName string, inv Inventory) {
	if len(inv.Commands) > 0 {
		b.WriteString("commands:\n")
		for _, cmd := range inv.Commands {
			desc := oneLine(cmd.Description)
			if desc == "" {
				desc = "(no description)"
			}
			invocation := "/" + pluginName + ":" + cmd.Name
			if cmd.ArgHint != "" {
				fmt.Fprintf(b, "  %s %s - %s\n", invocation, cmd.ArgHint, desc)
			} else {
				fmt.Fprintf(b, "  %s - %s\n", invocation, desc)
			}
		}
	}
	if len(inv.Skills) > 0 {
		b.WriteString("skills:\n")
		for _, sk := range inv.Skills {
			desc := oneLine(sk.Description)
			if desc == "" {
				desc = "(no description)"
			}
			invocation := "/" + pluginName + ":" + sk.Name
			if sk.RunAs != "" {
				fmt.Fprintf(b, "  %s [%s] - %s\n", invocation, sk.RunAs, desc)
			} else {
				fmt.Fprintf(b, "  %s - %s\n", invocation, desc)
			}
		}
	}
	if len(inv.Prompts) > 0 {
		b.WriteString("prompts:\n")
		for _, pr := range inv.Prompts {
			desc := oneLine(pr.Description)
			if desc == "" {
				desc = "(no description)"
			}
			if pr.ArgHint != "" {
				fmt.Fprintf(b, "  %s %s - %s\n", pr.Name, pr.ArgHint, desc)
			} else {
				fmt.Fprintf(b, "  %s - %s\n", pr.Name, desc)
			}
		}
	}
	if len(inv.Themes) > 0 {
		b.WriteString("themes:\n")
		for _, theme := range inv.Themes {
			fmt.Fprintf(b, "  %s - %s\n", theme.Name, theme.Path)
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
	if len(inv.Skills) == 0 && len(inv.Commands) == 0 && len(inv.Prompts) == 0 && len(inv.Themes) == 0 && len(inv.Hooks) == 0 && len(inv.MCPServers) == 0 {
		b.WriteString("capabilities: no detailed inventory available\n")
	}
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
