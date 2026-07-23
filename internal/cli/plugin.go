package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/installsource"
	"reasonix/internal/pluginpkg"
)

func pluginCommand(args []string) int {
	if len(args) == 0 {
		pluginUsage()
		return 0
	}
	switch args[0] {
	case "install":
		return pluginInstallCommand(args[1:])
	case "list":
		return pluginListCommand()
	case "show":
		return pluginShowCommand(args[1:])
	case "remove", "uninstall":
		return pluginRemoveCommand(args[1:])
	case "enable":
		return pluginSetEnabledCommand(args[1:], true)
	case "disable":
		return pluginSetEnabledCommand(args[1:], false)
	case "doctor":
		return pluginDoctorCommand(args[1:])
	case "help", "--help", "-h":
		pluginUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown plugin command %q\n\n", args[0])
		pluginUsage()
		return 2
	}
}

func pluginUsage() {
	fmt.Fprintln(os.Stderr, `usage:
  reasonix plugin install <source> [--yes] [--dry-run] [--link] [--replace]
  reasonix plugin list
  reasonix plugin show <name>
  reasonix plugin enable <name>
  reasonix plugin disable <name>
  reasonix plugin remove <name>
  reasonix plugin doctor <name>`)
}

func pluginInstallCommand(args []string) int {
	opts, source, err := parsePluginInstallArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if !opts.dryRun && !opts.yes {
		fmt.Fprintln(os.Stderr, "plugin install writes files; re-run with --yes to apply, or --dry-run to preview")
		return 2
	}
	mode := "copy"
	if opts.link {
		mode = "link"
	}
	body := map[string]any{
		"source":  source,
		"kind":    "plugin",
		"apply":   !opts.dryRun,
		"mode":    mode,
		"replace": opts.replace,
	}
	if strings.TrimSpace(opts.name) != "" {
		body["name"] = strings.TrimSpace(opts.name)
	}
	return runInstallSourceJSON(body)
}

type parsedPluginInstallArgs struct {
	yes     bool
	dryRun  bool
	link    bool
	replace bool
	name    string
}

func parsePluginInstallArgs(args []string) (parsedPluginInstallArgs, string, error) {
	var opts parsedPluginInstallArgs
	var source string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--yes":
			opts.yes = true
		case arg == "--dry-run":
			opts.dryRun = true
		case arg == "--link":
			opts.link = true
		case arg == "--replace":
			opts.replace = true
		case arg == "--name":
			i++
			if i >= len(args) {
				return opts, "", fmt.Errorf("--name requires a value")
			}
			opts.name = args[i]
		case strings.HasPrefix(arg, "--name="):
			opts.name = strings.TrimPrefix(arg, "--name=")
		case strings.HasPrefix(arg, "-"):
			return opts, "", fmt.Errorf("unknown plugin install flag %q", arg)
		default:
			if source != "" {
				return opts, "", fmt.Errorf("plugin install requires exactly one source")
			}
			source = arg
		}
	}
	if source == "" {
		return opts, "", fmt.Errorf("plugin install requires exactly one source")
	}
	return opts, source, nil
}

func pluginRemoveCommand(args []string) int {
	name, yes, err := parsePluginRemoveArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if !yes {
		fmt.Fprintln(os.Stderr, "plugin remove writes files; re-run with --yes to apply")
		return 2
	}
	return runInstallSourceJSON(map[string]any{"op": "uninstall", "kind": "plugin", "name": name, "scope": "global"})
}

func parsePluginRemoveArgs(args []string) (string, bool, error) {
	var name string
	var yes bool
	for _, arg := range args {
		switch arg {
		case "--yes":
			yes = true
		default:
			if strings.HasPrefix(arg, "-") {
				return "", false, fmt.Errorf("unknown plugin remove flag %q", arg)
			}
			if name != "" {
				return "", false, fmt.Errorf("plugin remove requires a plugin name")
			}
			name = arg
		}
	}
	if name == "" {
		return "", false, fmt.Errorf("plugin remove requires a plugin name")
	}
	return name, yes, nil
}

func runInstallSourceJSON(body map[string]any) int {
	raw, _ := json.Marshal(body)
	tl := installsource.NewTool(installsource.Options{})
	out, err := tl.Execute(context.Background(), raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(out)
	var resp struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !resp.OK {
		return 1
	}
	return 0
}

func pluginListCommand() int {
	st, err := pluginpkg.LoadState(config.ReasonixHomeDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(st.Plugins) == 0 {
		fmt.Println("no plugins installed")
		return 0
	}
	for _, p := range st.Plugins {
		state := "disabled"
		if p.Enabled {
			state = "enabled"
		}
		version := p.Version
		if version == "" {
			version = "-"
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", p.Name, state, version, p.Source)
	}
	return 0
}

func pluginShowCommand(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "plugin show requires a plugin name")
		return 2
	}
	p, ok, err := findInstalledPlugin(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "plugin %q is not installed\n", args[0])
		return 1
	}
	root := pluginpkg.ResolveRoot(config.ReasonixHomeDir(), p.Root)
	pkg, warnings, err := pluginpkg.ParseDir(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	skills, commands, hooks, mcp := pkg.CapabilityCounts()
	fmt.Printf("name: %s\nversion: %s\nenabled: %t\nkind: %s\nroot: %s\nsource: %s\nskills: %d\ncommands: %d\nhooks: %d\nmcpServers: %d\n",
		p.Name, p.Version, p.Enabled, p.ManifestKind, root, p.Source, skills, commands, hooks, mcp)
	printPluginInventory(p.Name, pkg.Inventory())
	for _, warning := range warnings {
		fmt.Println("warning:", warning)
	}
	return 0
}

func printPluginInventory(pluginName string, inv pluginpkg.Inventory) {
	if len(inv.Skills) > 0 {
		fmt.Println("usage:")
		fmt.Println("  skills are available in interactive sessions; run /skills to browse them, or invoke a skill directly with /<plugin>:<name>.")
		fmt.Println("skills:")
		for _, sk := range inv.Skills {
			desc := sk.Description
			if desc == "" {
				desc = "(no description)"
			}
			invocation := "/" + pluginName + ":" + sk.Name
			if sk.RunAs != "" {
				fmt.Printf("  %s\t%s\t%s\n", invocation, sk.RunAs, desc)
			} else {
				fmt.Printf("  %s\t%s\n", invocation, desc)
			}
		}
	}
	if len(inv.Commands) > 0 {
		fmt.Println("commands:")
		for _, cmd := range inv.Commands {
			desc := cmd.Description
			if desc == "" {
				desc = "(no description)"
			}
			invocation := "/" + pluginName + ":" + cmd.Name
			if cmd.ArgHint != "" {
				fmt.Printf("  %s %s\t%s\n", invocation, cmd.ArgHint, desc)
			} else {
				fmt.Printf("  %s\t%s\n", invocation, desc)
			}
		}
	}
	if len(inv.Hooks) > 0 {
		fmt.Println("hooks:")
		for _, hook := range inv.Hooks {
			target := hook.Command
			if target == "" {
				target = hook.ContextFile
			}
			match := hook.Match
			if match == "" {
				match = "*"
			}
			if hook.Description != "" {
				fmt.Printf("  %s\tmatch=%s\t%s\t%s\n", hook.Event, match, target, hook.Description)
			} else {
				fmt.Printf("  %s\tmatch=%s\t%s\n", hook.Event, match, target)
			}
		}
	}
	if len(inv.MCPServers) > 0 {
		fmt.Println("mcpServers:")
		for _, server := range inv.MCPServers {
			target := server.Command
			if target == "" {
				target = server.URL
			}
			fmt.Printf("  %s\t%s\t%s\n", server.Name, server.Transport, target)
		}
	}
}

func pluginDoctorCommand(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "plugin doctor requires a plugin name")
		return 2
	}
	p, ok, err := findInstalledPlugin(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "plugin %q is not installed\n", args[0])
		return 1
	}
	root := pluginpkg.ResolveRoot(config.ReasonixHomeDir(), p.Root)
	pkg, warnings, err := pluginpkg.ParseDir(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid:", err)
		return 1
	}
	for _, skillRoot := range pkg.SkillRoots() {
		if st, err := os.Stat(skillRoot); err != nil || !st.IsDir() {
			fmt.Fprintf(os.Stderr, "missing skill root: %s\n", skillRoot)
			return 1
		}
	}
	for _, commandRoot := range pkg.CommandRoots() {
		if st, err := os.Stat(commandRoot); err != nil || !st.IsDir() {
			fmt.Fprintf(os.Stderr, "missing command root: %s\n", commandRoot)
			return 1
		}
	}
	for _, warning := range warnings {
		fmt.Println("warning:", warning)
	}
	fmt.Printf("ok: %s (%s)\n", p.Name, filepath.Clean(root))
	return 0
}

func pluginSetEnabledCommand(args []string, enabled bool) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "plugin enable/disable requires a plugin name")
		return 2
	}
	if err := pluginpkg.SetEnabled(config.ReasonixHomeDir(), args[0], enabled); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("%s %s\n", map[bool]string{true: "enabled", false: "disabled"}[enabled], args[0])
	return 0
}

func findInstalledPlugin(name string) (pluginpkg.InstalledPlugin, bool, error) {
	st, err := pluginpkg.LoadState(config.ReasonixHomeDir())
	if err != nil {
		return pluginpkg.InstalledPlugin{}, false, err
	}
	for _, p := range st.Plugins {
		if p.Name == name {
			return p, true, nil
		}
	}
	return pluginpkg.InstalledPlugin{}, false, nil
}
