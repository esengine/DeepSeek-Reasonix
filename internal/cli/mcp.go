package cli

import (
	"fmt"
	"os"
	"strings"

	"reasonix/internal/codegraph"
	"reasonix/internal/config"
)

// mcp.go holds the MCP server-management surface shared by the `reasonix mcp`
// subcommand (config-only; takes effect next session) and the in-chat `/mcp add`
// / `/mcp remove` slash commands (which hot-connect via the controller). Both
// parse arguments through parseMCPAdd so the grammar is identical everywhere.

// parseMCPAdd turns the arguments after "add" into a config.PluginEntry. Grammar:
//
//	<name> [--http URL | --sse URL] [--env K=V]... [--header K=V]... [command [args...]]
//
// A --http/--sse URL makes it a remote server; otherwise the first non-flag token
// (after the name and any --env/--header flags) begins the stdio command, and the
// rest are its args verbatim — so the command keeps its own -flags (e.g. `npx -y
// pkg`). Flag values accept both "--http URL" and "--http=URL" forms.
func parseMCPAdd(args []string) (config.PluginEntry, error) {
	var e config.PluginEntry
	if len(args) == 0 {
		return e, fmt.Errorf("mcp add: missing server name")
	}
	e.Name = strings.TrimSpace(args[0])
	if e.Name == "" || strings.HasPrefix(e.Name, "-") {
		return e, fmt.Errorf("mcp add: first argument must be the server name, got %q", args[0])
	}
	rest := args[1:]

	i := 0
	// next consumes the following token as a flag's value (for the "--flag value"
	// form), reporting false when none remains.
	next := func(flag string) (string, error) {
		if i+1 >= len(rest) {
			return "", fmt.Errorf("mcp add: %s needs a value", flag)
		}
		i++
		return rest[i], nil
	}
	setEnv := func(dst *map[string]string, flag, pair string) error {
		k, v, ok := strings.Cut(pair, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return fmt.Errorf("mcp add: %s expects KEY=VALUE, got %q", flag, pair)
		}
		if *dst == nil {
			*dst = map[string]string{}
		}
		(*dst)[k] = v
		return nil
	}

	for ; i < len(rest); i++ {
		a := rest[i]
		key, inline, hasInline := strings.Cut(a, "=")
		switch {
		case !strings.HasPrefix(a, "-"):
			// The stdio command and its remaining args, verbatim.
			e.Command = a
			e.Args = append([]string(nil), rest[i+1:]...)
			i = len(rest)
		case key == "--http" || key == "--streamable-http":
			v := inline
			if !hasInline {
				var err error
				if v, err = next(key); err != nil {
					return e, err
				}
			}
			e.Type, e.URL = "http", v
		case key == "--sse":
			v := inline
			if !hasInline {
				var err error
				if v, err = next(key); err != nil {
					return e, err
				}
			}
			e.Type, e.URL = "sse", v
		case key == "--env" || key == "--header":
			pair := inline
			if !hasInline {
				var err error
				if pair, err = next(key); err != nil {
					return e, err
				}
			}
			dst := &e.Env
			if key == "--header" {
				dst = &e.Headers
			}
			if err := setEnv(dst, key, pair); err != nil {
				return e, err
			}
		default:
			return e, fmt.Errorf("mcp add: unknown flag %q", a)
		}
	}

	switch {
	case e.URL != "" && e.Command != "":
		return e, fmt.Errorf("mcp add: specify a command OR a --http/--sse URL, not both")
	case e.URL == "" && e.Command == "":
		return e, fmt.Errorf("mcp add: need a command (stdio) or a --http/--sse URL")
	}
	return e, nil
}

// tokenizeArgs splits a slash-command line into arguments, honouring "double" and
// 'single' quotes so values with spaces (e.g. --header "Authorization=Bearer x")
// survive. An unterminated quote takes the rest of the line as one token.
func tokenizeArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inWord := false
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
			inWord = true
		case r == '"' || r == '\'':
			quote = r
			inWord = true
		case r == ' ' || r == '\t':
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out
}

// mcpCommand implements `reasonix mcp <add|remove|list>`. It edits config only
// (validate → UpsertPlugin/RemovePlugin → Save); the server connects on the next
// session start. For a live connect inside an open chat, use `/mcp add`.
func mcpCommand(args []string) int {
	if len(args) == 0 {
		mcpUsage()
		return 2
	}
	switch args[0] {
	case "list", "ls":
		return mcpList()
	case "add":
		return mcpAddCLI(args[1:])
	case "remove", "rm":
		return mcpRemoveCLI(args[1:])
	case "import":
		return mcpImportCLI()
	case "help", "-h", "--help":
		mcpUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown mcp subcommand %q\n\n", args[0])
		mcpUsage()
		return 2
	}
}

func mcpImportCLI() int {
	total, added, updated, err := config.ImportCCSwitchMCP()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("imported %d MCP servers from cc-switch (%d added, %d updated) — servers load on the next session\n", total, added, updated)
	return 0
}

func mcpList() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	listed := 0
	// CodeGraph is a built-in server injected by boot, not a [[plugins]] entry, so
	// report its resolved status here too. It is listed even when disabled, matching
	// the MCP manager where the user can enable it.
	codegraphMeta := fmt.Sprintf(" [enabled=%v auto_install=%v]", cfg.Codegraph.Enabled, cfg.Codegraph.AutoInstall)
	if bin, ok := codegraph.Resolve(cfg.Codegraph.Path); ok {
		fmt.Printf("%-16s (stdio, built-in)%s  %s serve --mcp\n", "codegraph", codegraphMeta, bin)
	} else {
		fmt.Printf("%-16s (built-in, not installed)%s  run `reasonix codegraph install`", "codegraph", codegraphMeta)
		if cfg.Codegraph.Enabled && cfg.Codegraph.AutoInstall {
			fmt.Print(" (or let auto_install fetch it on next startup)")
		}
		fmt.Println()
	}
	listed++
	for _, p := range cfg.Plugins {
		typ := p.Type
		if typ == "" {
			typ = "stdio"
		}
		auto := ""
		if !p.ShouldAutoStart() {
			auto = " [auto_start=false]"
		}
		if typ == "stdio" {
			line := strings.TrimSpace(p.Command + " " + strings.Join(p.Args, " "))
			fmt.Printf("%-16s (stdio)%s  %s\n", p.Name, auto, line)
		} else {
			fmt.Printf("%-16s (%s)%s  %s\n", p.Name, typ, auto, p.URL)
		}
		listed++
	}
	if listed == 0 {
		fmt.Println("no MCP servers configured")
	}
	return 0
}

func mcpAddCLI(args []string) int {
	entry, err := parseMCPAdd(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := cfg.UpsertPlugin(entry); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := cfg.Save(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("added MCP server %q — loads on the next session (or run `/mcp add` inside chat to connect it live now)\n", entry.Name)
	return 0
}

func mcpRemoveCLI(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix mcp remove <name>")
		return 2
	}
	name := args[0]
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !cfg.RemovePlugin(name) {
		fmt.Fprintf(os.Stderr, "no MCP server named %q in config\n", name)
		return 1
	}
	if err := cfg.Save(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("removed MCP server %q\n", name)
	return 0
}

func mcpUsage() {
	fmt.Println(`Manage MCP servers (persisted to reasonix.toml).

Usage:
  reasonix mcp list
  reasonix mcp add <name> <command> [args...]        stdio server
  reasonix mcp add <name> --http <url> [--header K=V] remote (Streamable HTTP)
  reasonix mcp add <name> --sse  <url>               remote (legacy SSE)
  reasonix mcp import                                import Codex-enabled servers from cc-switch
  reasonix mcp remove <name>

Flags for add:
  --http <url> | --sse <url>   remote transport (omit for a stdio command)
  --env K=V                    set an environment variable (repeatable, stdio)
  --header K=V                 set an HTTP header (repeatable, remote)

Examples:
  reasonix mcp add fs npx -y @modelcontextprotocol/server-filesystem .
  reasonix mcp add stripe --http https://mcp.stripe.com --header "Authorization=Bearer $STRIPE_KEY"

Changes take effect on the next session; inside a running chat, use /mcp add to
connect a server live.`)
}
