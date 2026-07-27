package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/mcpregistry"
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
		return e, fmt.Errorf("mcp add: missing server name, command, or URL")
	}

	// Simplified forms:
	//   reasonix mcp add -- npx -y chrome-devtools-mcp@latest
	//   reasonix mcp add https://example.com/mcp
	// keep the historical "name command..." form as well.
	if args[0] == "--" {
		if len(args) < 2 {
			return e, fmt.Errorf("mcp add: -- requires a command argv")
		}
		e.Command = args[1]
		e.Args = append([]string(nil), args[2:]...)
		e.Name = defaultMCPNameFromArgv(e.Command, e.Args)
		if e.Name == "" {
			return e, fmt.Errorf("mcp add: could not derive a server name from the command; pass an explicit name")
		}
		return e, nil
	}
	if looksLikeRemoteMCPURL(args[0]) && (len(args) == 1 || strings.HasPrefix(args[1], "-")) {
		e.Name = defaultMCPNameFromURL(args[0])
		e.Type, e.URL = "http", args[0]
		// Allow trailing --header/--env after a bare URL.
		if len(args) > 1 {
			restEntry, err := parseMCPAdd(append([]string{e.Name, "--http", args[0]}, args[1:]...))
			if err != nil {
				return e, err
			}
			return restEntry, nil
		}
		return e, nil
	}

	e.Name = strings.TrimSpace(args[0])
	if e.Name == "" || strings.HasPrefix(e.Name, "-") {
		return e, fmt.Errorf("mcp add: first argument must be the server name, got %q", args[0])
	}
	rest := args[1:]
	if len(rest) > 0 && rest[0] == "--" {
		// reasonix mcp add <name> -- <argv...>
		if len(rest) < 2 {
			return e, fmt.Errorf("mcp add: -- requires a command argv")
		}
		e.Command = rest[1]
		e.Args = append([]string(nil), rest[2:]...)
		return e, nil
	}

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

func looksLikeRemoteMCPURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
}

func defaultMCPNameFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "remote-mcp"
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	host = strings.Split(host, ".")[0]
	host = sanitizeMCPName(host)
	if host == "" {
		return "remote-mcp"
	}
	return host
}

func defaultMCPNameFromArgv(command string, args []string) string {
	runner := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(filepath.Base(command), ".exe"), ".cmd"), ".bat"))
	candidate := command
	switch runner {
	case "npx", "bunx", "uvx":
		if operand := firstMCPCommandOperand(args); operand != "" {
			candidate = operand
		}
	case "python", "python3", "py":
		for i, arg := range args {
			if arg == "-m" && i+1 < len(args) {
				candidate = args[i+1]
				break
			}
		}
		if candidate == command {
			if operand := firstMCPCommandOperand(args); operand != "" {
				candidate = operand
			}
		}
	case "node":
		if operand := firstMCPCommandOperand(args); operand != "" {
			candidate = operand
		}
	case "uv":
		if len(args) > 0 && args[0] == "run" {
			if operand := firstMCPCommandOperand(args[1:]); operand != "" {
				candidate = operand
			}
		}
	}
	base := filepath.Base(candidate)
	if at := strings.Index(base, "@"); at > 0 {
		base = base[:at]
	}
	for _, ext := range []string{".js", ".exe", ".cmd", ".bat"} {
		base = strings.TrimSuffix(base, ext)
	}
	name := sanitizeMCPName(base)
	if name == "" {
		return "mcp-server"
	}
	if candidate == command {
		switch runner {
		case "npx", "bunx", "uvx", "uv", "node", "python", "python3", "py":
			return "mcp-server"
		}
	}
	return name
}

func firstMCPCommandOperand(args []string) string {
	valueFlags := map[string]bool{
		"-p": true, "--package": true, "-c": true, "--call": true,
		"--node-options": true, "--python": true,
	}
	options := true
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if options && arg == "--" {
			options = false
			continue
		}
		if options && strings.HasPrefix(arg, "-") {
			if valueFlags[arg] {
				i++
			}
			continue
		}
		if arg != "" {
			return arg
		}
	}
	return ""
}

func sanitizeMCPName(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	return name
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

// mcpCommand implements persisted server management plus explicit browse/install
// access to the official MCP Registry. Config edits take effect on the next
// session start; for a live manual connection inside an open chat, use `/mcp add`.
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
	case "enable":
		return mcpEnableCLI(args[1:], true)
	case "disable":
		return mcpEnableCLI(args[1:], false)
	case "retry", "connect":
		// connect remains a compatibility alias for enable/retry.
		return mcpRetryCLI(args[1:])
	case "update":
		return mcpUpdateCLI(args[1:])
	case "import":
		return mcpImportCLI()
	case "browse", "search":
		return mcpBrowseCLI(args[1:])
	case "install":
		return mcpInstallCLI(args[1:])
	case "help", "-h", "--help":
		mcpUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown mcp subcommand %q\n\n", args[0])
		mcpUsage()
		return 2
	}
}

func defaultMCPRegistryClient() *mcpregistry.Client {
	cachePath := ""
	if cacheDir := config.CacheDir(); cacheDir != "" {
		cachePath = filepath.Join(cacheDir, "mcp-registry-v0.1.json")
	}
	return mcpregistry.New(cachePath)
}

func mcpBrowseCLI(args []string) int {
	return mcpBrowseWithClient(args, defaultMCPRegistryClient())
}

func mcpBrowseWithClient(args []string, client *mcpregistry.Client) int {
	query := ""
	limit := 20
	jsonOutput := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			jsonOutput = true
		case "--limit":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "mcp browse: --limit needs a value")
				return 2
			}
			i++
			value, err := strconv.Atoi(args[i])
			if err != nil || value <= 0 || value > 100 {
				fmt.Fprintln(os.Stderr, "mcp browse: --limit must be between 1 and 100")
				return 2
			}
			limit = value
		default:
			if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "mcp browse: unknown flag %q\n", args[i])
				return 2
			}
			if query != "" {
				fmt.Fprintln(os.Stderr, "mcp browse: provide at most one search query")
				return 2
			}
			query = args[i]
		}
	}
	result, err := client.Search(context.Background(), query, limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if result.Warning != "" {
		fmt.Fprintf(os.Stderr, "MCP Registry unavailable; showing cached results: %s\n", result.Warning)
	}
	if jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result.Entries); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if len(result.Entries) == 0 {
		fmt.Println("no MCP Registry servers matched")
		return 0
	}
	for _, entry := range result.Entries {
		status := entry.Transport
		if !entry.Installable {
			status = "manual setup: " + entry.UnavailableReason
		}
		title := entry.Title
		if title == "" {
			title = entry.Name
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", entry.Name, entry.Version, status, title)
	}
	return 0
}

func mcpInstallCLI(args []string) int {
	return mcpInstallWithClient(args, defaultMCPRegistryClient())
}

func mcpInstallWithClient(args []string, client *mcpregistry.Client) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix mcp install <registry-name> [--as <local-name>]")
		return 2
	}
	registryName := strings.TrimSpace(args[0])
	if registryName == "" || strings.HasPrefix(registryName, "-") {
		fmt.Fprintln(os.Stderr, "mcp install: registry server name is required")
		return 2
	}
	localName := ""
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--as":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				fmt.Fprintln(os.Stderr, "mcp install: --as needs a local name")
				return 2
			}
			i++
			localName = strings.TrimSpace(args[i])
		default:
			fmt.Fprintf(os.Stderr, "mcp install: unknown argument %q\n", args[i])
			return 2
		}
	}
	entry, result, err := client.Resolve(context.Background(), registryName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if result.Warning != "" {
		fmt.Fprintf(os.Stderr, "MCP Registry unavailable; using cached result: %s\n", result.Warning)
	}
	pluginEntry, err := entry.PluginEntry(localName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, configured := range cfg.Plugins {
		if configured.Name == pluginEntry.Name {
			fmt.Fprintf(os.Stderr, "MCP server %q is already configured; choose another name with --as or remove it first\n", pluginEntry.Name)
			return 1
		}
	}
	installResult, probeErr := mcpProbeForInstall(pluginEntry)
	if probeErr != nil && installResult.State != "action_required" {
		fmt.Fprintf(os.Stderr, "MCP server %q was not installed: %s\n", pluginEntry.Name, installResult.Message)
		return 1
	}
	if err := persistCLIInstalledMCP(mcpCLIWorkspaceRoot(), pluginEntry); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if installResult.State == "action_required" {
		fmt.Printf("installed MCP Registry server %q as %q — authentication required; finish authentication and run `reasonix mcp retry %s`\n", entry.Name, pluginEntry.Name, pluginEntry.Name)
		return 0
	}
	fmt.Printf("installed MCP Registry server %q as %q — ready with %d tools\n", entry.Name, pluginEntry.Name, installResult.ToolCount)
	return 0
}

func mcpEnableCLI(args []string, enabled bool) int {
	if len(args) == 0 {
		action := "enable"
		if !enabled {
			action = "disable"
		}
		fmt.Fprintf(os.Stderr, "usage: reasonix mcp %s <name>\n", action)
		return 2
	}
	name := strings.TrimSpace(args[0])
	workspace := mcpCLIWorkspaceRoot()
	cfg, err := config.LoadForRoot(workspace)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var entry config.PluginEntry
	found := false
	for _, p := range cfg.Plugins {
		if p.Name == name {
			entry = p
			found = true
			break
		}
	}
	if !found {
		fmt.Fprintf(os.Stderr, "no MCP server named %q in config\n", name)
		return 1
	}
	store := config.DefaultMCPActivationStore()
	if err := store.SetServerEnabled(entry, workspace, enabled); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if enabled {
		fmt.Printf("enabled MCP server %q — tools restore from cache; process starts on first call\n", name)
	} else {
		fmt.Printf("disabled MCP server %q — tools removed from the catalog; authorization retained\n", name)
	}
	return 0
}

func mcpRetryCLI(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix mcp retry <name>")
		return 2
	}
	// Standalone CLI cannot talk to a live Host; enabling is the durable
	// equivalent of "retry next session". In-chat /mcp retry remains live.
	return mcpEnableCLI(args, true)
}

func mcpUpdateCLI(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix mcp update <name>")
		return 2
	}
	name := strings.TrimSpace(args[0])
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var entry config.PluginEntry
	found := false
	for _, configured := range cfg.Plugins {
		if configured.Name == name {
			entry, found = configured, true
			break
		}
	}
	if !found {
		fmt.Fprintf(os.Stderr, "no MCP server named %q in config\n", name)
		return 1
	}
	result, probeErr := mcpProbeForInstall(entry)
	if probeErr != nil {
		fmt.Fprintf(os.Stderr, "MCP update for %q was not applied: %s\n", name, result.Message)
		return 1
	}
	fmt.Printf("updated MCP server %q — candidate handshake passed with %d tools; cached schema switched atomically\n", name, result.ToolCount)
	return 0
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
	for _, configured := range cfg.Plugins {
		if configured.Name == entry.Name {
			fmt.Fprintf(os.Stderr, "MCP server %q is already configured; remove it first or choose another name\n", entry.Name)
			return 1
		}
	}
	result, probeErr := mcpProbeForInstall(entry)
	if probeErr != nil && result.State != "action_required" {
		fmt.Fprintf(os.Stderr, "MCP server %q was not added: %s\n", entry.Name, result.Message)
		return 1
	}
	if err := persistCLIInstalledMCP(mcpCLIWorkspaceRoot(), entry); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if result.State == "action_required" {
		fmt.Printf("added MCP server %q — authentication required; finish authentication and retry\n", entry.Name)
		return 0
	}
	fmt.Printf("added MCP server %q — ready with %d tools\n", entry.Name, result.ToolCount)
	return 0
}

var mcpProbeForInstall = probeMCPReadiness

func probeMCPReadiness(entry config.PluginEntry) (plugin.MCPInstallResult, error) {
	entry.Source = config.MCPSourceUserConfig
	workspace, err := os.Getwd()
	if err != nil {
		workspace = ""
	}
	specs := boot.PluginSpecsForRootWithOptions([]config.PluginEntry{entry}, workspace, boot.PluginSpecOptions{
		DefaultCallTimeout: 30 * time.Second,
		ConfigSource:       string(config.MCPSourceUserConfig),
		StateHome:          config.ReasonixHomeDir(),
		Network:            true,
	})
	if len(specs) != 1 {
		err := fmt.Errorf("could not build MCP launch specification")
		return plugin.InstallResultForError(entry.Name, err), err
	}
	host := plugin.NewHost()
	defer host.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return host.InstallAndConnect(ctx, specs[0])
}

func persistCLIInstalledMCP(workspace string, entry config.PluginEntry) error {
	entry.Source = config.MCPSourceUserConfig
	if _, err := config.UpsertPluginInSourceForRoot(workspace, entry); err != nil {
		return err
	}
	store := config.DefaultMCPActivationStore()
	activationErr := store.SetServerEnabled(entry, workspace, true)
	if !entry.ShouldAutoStart() {
		activationErr = store.ClearServer(entry, workspace)
	}
	if activationErr != nil {
		_, _, rollbackErr := config.RemovePluginFromSourceForRoot(workspace, entry)
		return errors.Join(activationErr, rollbackErr)
	}
	return nil
}

func mcpRemoveCLI(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix mcp remove <name>")
		return 2
	}
	name := args[0]
	workspace := mcpCLIWorkspaceRoot()
	removed, ok, _, err := config.RemovePluginFromEffectiveSourceForRoot(workspace, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "no MCP server named %q in config\n", name)
		return 1
	}
	// Uninstall clears activation overrides; schema/auth cleanup is handled by
	// the live session path when present.
	_ = config.DefaultMCPActivationStore().ClearServer(removed, workspace)
	fmt.Printf("removed MCP server %q\n", name)
	return 0
}

func mcpCLIWorkspaceRoot() string {
	if cwd, err := os.Getwd(); err == nil && strings.TrimSpace(cwd) != "" {
		return cwd
	}
	return "."
}

func mcpUsage() {
	fmt.Println(`Manage MCP servers (global installs use config.toml; project entries stay in project config).

Usage:
  reasonix mcp list
  reasonix mcp get <name>
  reasonix mcp install <registry-name> [--as <name>]
  reasonix mcp add -- <command> [args...]            stdio argv (no shell)
  reasonix mcp add <name> -- <command> [args...]
  reasonix mcp add <name> <command> [args...]        legacy stdio form
  reasonix mcp add https://example.com/mcp           remote HTTP
  reasonix mcp add <name> --http <url> [--header K=V]
  reasonix mcp add <name> --sse  <url>
  reasonix mcp enable <name>
  reasonix mcp disable <name>
  reasonix mcp retry <name>
  reasonix mcp update <name>
  reasonix mcp browse [query] [--limit N] [--json]
  reasonix mcp import
  reasonix mcp remove <name>

Flags for add:
  --http <url> | --sse <url>   remote transport (omit for a stdio command)
  --env K=V                    set an environment variable (repeatable, stdio)
  --header K=V                 set an HTTP header (repeatable, remote)

Examples:
  reasonix mcp add fs npx -y @modelcontextprotocol/server-filesystem .
  reasonix mcp add stripe --http https://mcp.stripe.com --header "Authorization=Bearer $STRIPE_KEY"

CLI config changes take effect on the next session. Inside a running chat, use
/mcp add to save and connect a server immediately. Installing a server is also
its authorization; there is no separate trust step.

Servers declared by project reasonix.toml or .mcp.json are trusted configuration
and need no separate launch confirmation. Project entries override same-name
global entries; within a project, reasonix.toml overrides .mcp.json. Writer or
destructive annotations never trigger per-call approval. Explicit deny rules
still win; Plan Mode and strict read-only subagents may filter which tools are
available.`)
}
