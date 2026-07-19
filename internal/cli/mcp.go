package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/mcpcatalog"
	"reasonix/internal/mcptrust"
	"reasonix/internal/plugin"
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
	case "get":
		return mcpGetCLI(args[1:])
	case "remove", "rm":
		return mcpRemoveCLI(args[1:])
	case "import":
		return mcpImportCLI()
	case "trust":
		return mcpTrustCLI(args[1:])
	case "untrust":
		return mcpUntrustCLI(args[1:])
	case "verify":
		return mcpVerifyCLI(args[1:])
	case "catalog":
		return mcpCatalogCLI(args[1:])
	case "help", "-h", "--help":
		mcpUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown mcp subcommand %q\n\n", args[0])
		mcpUsage()
		return 2
	}
}

func mcpTrustCLI(args []string) int {
	if len(args) < 2 || (args[1] != "--session" && args[1] != "--workspace") {
		fmt.Fprintln(os.Stderr, "usage: reasonix mcp trust <name> --session|--workspace")
		return 2
	}
	spec, err := mcpSecuritySpec(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	decision := strings.TrimPrefix(args[1], "--")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := plugin.SetSpecTrust(ctx, spec, decision); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("trusted MCP server %q for %s (reader snapshots only; writers remain approval-gated)\n", spec.Name, decision)
	return 0
}

func mcpUntrustCLI(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: reasonix mcp untrust <name>")
		return 2
	}
	spec, err := mcpSecuritySpec(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := plugin.SetSpecTrust(context.Background(), spec, "revoke"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("revoked MCP trust for %q\n", spec.Name)
	return 0
}

func mcpVerifyCLI(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: reasonix mcp verify <name>")
		return 2
	}
	spec, err := mcpSecuritySpec(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	inspection, err := plugin.InspectSpec(ctx, spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("name: %s\ntrust: %s\nisolation: %s\nreaders: %s\nwriters: %s\ndestructive: %s\n",
		spec.Name, inspection.Security.TrustState, inspection.Security.IsolationState,
		strings.Join(inspection.Readers, ", "), strings.Join(inspection.Writers, ", "), strings.Join(inspection.Destructive, ", "))
	if len(inspection.Security.ChangedTools) > 0 {
		fmt.Printf("changed_tools: %s\n", strings.Join(inspection.Security.ChangedTools, ", "))
	}
	return 0
}

func mcpCatalogCLI(args []string) int {
	if len(args) != 1 || args[0] != "refresh" {
		fmt.Fprintln(os.Stderr, "usage: reasonix mcp catalog refresh")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := (mcpcatalog.Loader{CacheDir: config.CacheDir()}).Load(ctx, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("MCP catalog sequence %d (%s", result.Index.Sequence, result.Source)
	if result.Offline {
		fmt.Print(", offline verified snapshot")
	}
	if result.Stale {
		fmt.Print(", stale snapshot")
	}
	fmt.Println(")")
	return 0
}

func mcpSecuritySpec(name string) (plugin.Spec, error) {
	root, err := os.Getwd()
	if err != nil {
		return plugin.Spec{}, err
	}
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return plugin.Spec{}, err
	}
	var entry *config.PluginEntry
	for i := range cfg.Plugins {
		if cfg.Plugins[i].Name == name {
			entry = &cfg.Plugins[i]
			break
		}
	}
	if entry == nil {
		return plugin.Spec{}, fmt.Errorf("no MCP server named %q in config", name)
	}
	specs := boot.PluginSpecsForRootWithOptions([]config.PluginEntry{*entry}, root, boot.PluginSpecOptions{
		DefaultCallTimeout: time.Duration(cfg.MCPCallTimeoutSeconds()) * time.Second,
		TrustManager:       mcptrust.ForWorkspace(config.ReasonixHomeDir(), root), ConfigSource: "workspace_config",
		StateHome: config.ReasonixHomeDir(), WriterRoots: cfg.WriteRootsForRoot(root),
		ForbidReadRoots: boot.RuntimeForbidReadRoots(cfg, root), Network: cfg.Sandbox.Network,
		OfficialServers: boot.LoadOfficialMCPTrust(context.Background(), cfg),
	})
	if len(specs) != 1 {
		return plugin.Spec{}, fmt.Errorf("failed to build MCP server %q", name)
	}
	return specs[0], nil
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

func mcpGetCLI(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix mcp get <name>")
		return 2
	}
	name := args[0]
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, p := range cfg.Plugins {
		if p.Name != name {
			continue
		}
		printMCPEntry(p)
		return 0
	}
	fmt.Fprintf(os.Stderr, "no MCP server named %q in config\n", name)
	return 1
}

func printMCPEntry(p config.PluginEntry) {
	typ := p.Type
	if typ == "" {
		typ = "stdio"
	}
	fmt.Printf("name: %s\n", p.Name)
	fmt.Printf("type: %s\n", typ)
	if typ == "stdio" {
		fmt.Printf("command: %s\n", p.Command)
		if len(p.Args) > 0 {
			fmt.Printf("args: %s\n", strings.Join(p.Args, "\n      "))
		}
		if len(p.Env) > 0 {
			fmt.Println("env:")
			for _, k := range sortedMapKeys(p.Env) {
				fmt.Printf("  %s=%s\n", k, redactMCPConfigValue(k, p.Env[k]))
			}
		}
	} else {
		fmt.Printf("url: %s\n", redactMCPURL(p.URL))
		if len(p.Headers) > 0 {
			fmt.Println("headers:")
			for _, k := range sortedMapKeys(p.Headers) {
				fmt.Printf("  %s=%s\n", k, redactMCPConfigValue(k, p.Headers[k]))
			}
		}
	}
	if !p.ShouldAutoStart() {
		fmt.Println("auto_start: false")
	}
}

func sortedMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func redactMCPConfigValue(key, value string) string {
	if looksSensitiveMCPKey(key) || looksSensitiveMCPValue(value) {
		return "<redacted>"
	}
	return value
}

func looksSensitiveMCPKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, needle := range []string{"auth", "token", "secret", "credential", "api_key", "api-key", "apikey", "cookie"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func looksSensitiveMCPQueryKey(key string) bool {
	return strings.EqualFold(strings.TrimSpace(key), "key") || looksSensitiveMCPKey(key)
}

func looksSensitiveMCPValue(value string) bool {
	lower := strings.ToLower(value)
	for _, needle := range []string{"access_token", "id_token", "refresh_token", "api_key", "api-key", "apikey", "bearer "} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func redactMCPURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}
	u, err := url.Parse(trimmed)
	if err != nil || u == nil {
		if looksSensitiveMCPValue(raw) {
			return "<redacted>"
		}
		return raw
	}
	q := u.Query()
	changed := false
	for key := range q {
		if looksSensitiveMCPQueryKey(key) {
			q.Set(key, "<redacted>")
			changed = true
		}
	}
	if !changed {
		return raw
	}
	u.RawQuery = q.Encode()
	return u.String()
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
  reasonix mcp get <name>
  reasonix mcp add <name> <command> [args...]        stdio server
  reasonix mcp add <name> --http <url> [--header K=V] remote (Streamable HTTP)
  reasonix mcp add <name> --sse  <url>               remote (legacy SSE)
  reasonix mcp import                                import MCP servers from cc-switch
  reasonix mcp remove <name>
  reasonix mcp verify <name>
  reasonix mcp trust <name> --session|--workspace
  reasonix mcp untrust <name>
  reasonix mcp catalog refresh

Flags for add:
  --http <url> | --sse <url>   remote transport (omit for a stdio command)
  --env K=V                    set an environment variable (repeatable, stdio)
  --header K=V                 set an HTTP header (repeatable, remote)

Examples:
  reasonix mcp add fs npx -y @modelcontextprotocol/server-filesystem .
  reasonix mcp add stripe --http https://mcp.stripe.com --header "Authorization=Bearer $STRIPE_KEY"

Changes take effect on the next session; inside a running chat, use /mcp add to
connect a server live.

Third-party readOnlyHint metadata becomes reader authority only after the exact
server identity and tool snapshot is trusted. MCP writers are blocked in Plan;
destructive tools require a fresh user decision on every call. stdio servers run
inside the MCP sandbox when available and are reported as unisolated otherwise.`)
}
