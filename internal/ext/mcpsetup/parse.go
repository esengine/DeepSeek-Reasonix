package mcpsetup

import (
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/config"
)

// Draft is a parsed proposal: what would be installed, and what the user should
// look at before agreeing to it.
type Draft struct {
	Entries []config.PluginEntry
	Risks   []Risk
}

// Risk is something true about the draft that the user has to decide about. It
// is never fatal — a pasted secret still installs if that is what they want.
type Risk struct {
	Server string
	Kind   string // secret | shell | unknown-host
	Field  string
	Detail string
}

// Parse accepts the three shapes an MCP server is handed out in: the mcpServers
// JSON block from its docs, a command line to run it, or the URL of a hosted
// one. Which shape it is can be told from the first non-space character, so the
// user never has to say.
func Parse(input string) (Draft, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return Draft{}, fmt.Errorf("粘贴一段 JSON、一行命令，或者一个 https 地址")
	}
	var entries []config.PluginEntry
	var err error
	switch {
	case strings.HasPrefix(trimmed, "{"):
		entries, err = config.ParseMCPServersJSON([]byte(trimmed))
	case looksLikeRemoteURL(trimmed):
		entries, err = parseCommandLine(trimmed)
	default:
		entries, err = parseCommandLine(stripShellPrompt(trimmed))
	}
	if err != nil {
		return Draft{}, secrets.DiagnosticError(err)
	}
	for i := range entries {
		if strings.TrimSpace(entries[i].Name) == "" {
			return Draft{}, fmt.Errorf("这段配置里没有服务名，也推不出一个来")
		}
	}
	return Draft{Entries: entries, Risks: risksFor(entries)}, nil
}

// parseCommandLine handles both the bare `npx -y foo` a README prints and the
// full `reasonix mcp add` argv, so a user pasting either gets the same answer.
func parseCommandLine(line string) ([]config.PluginEntry, error) {
	args := Tokenize(line)
	if len(args) == 0 {
		return nil, fmt.Errorf("这行里没有可执行的命令")
	}
	// A README's command is an argv, not a name followed by an argv. Prefixing
	// "--" is what tells ParseArgs so, and it derives the name from the package.
	launcher := strings.TrimSuffix(strings.ToLower(filepath.Base(args[0])), ".exe")
	if len(args) >= 3 && launcher == "reasonix" && args[1] == "mcp" && args[2] == "add" {
		args = args[3:]
	} else if args[0] != "--" && !looksLikeRemoteURL(args[0]) {
		args = append([]string{"--"}, args...)
	}
	entry, err := ParseArgs(args)
	if err != nil {
		return nil, err
	}
	return []config.PluginEntry{entry}, nil
}

// stripShellPrompt drops the "$ " or "> " a copied terminal line carries.
func stripShellPrompt(line string) string {
	for _, p := range []string{"$ ", "> ", "% "} {
		if after, ok := strings.CutPrefix(line, p); ok {
			return strings.TrimSpace(after)
		}
	}
	return line
}

// risksFor reports what an install would expose. A secret pasted as a literal is
// the common one: it ends up in a config file that gets backed up, synced and
// screenshotted, while ${VAR} keeps it in the environment.
func risksFor(entries []config.PluginEntry) []Risk {
	var out []Risk
	for _, e := range entries {
		// What it does comes before what it costs: the user has to know this
		// starts a process (or talks to a host) before judging the credential.
		if cmd := strings.TrimSpace(e.Command); cmd != "" {
			out = append(out, Risk{Server: e.Name, Kind: "shell", Field: "command",
				Detail: strings.TrimSpace(secrets.RedactConfigValue("", cmd) + " " + strings.Join(secrets.RedactArgs(e.Args), " "))})
		}
		if u := strings.TrimSpace(e.URL); u != "" {
			out = append(out, Risk{Server: e.Name, Kind: "unknown-host", Field: "url", Detail: RedactURL(u)})
		}
		for _, k := range sortedKeys(e.Env) {
			if literalSecret(k, e.Env[k]) {
				out = append(out, Risk{Server: e.Name, Kind: "secret", Field: "env." + k,
					Detail: "明文写进配置文件；改成 ${" + envVarName(k) + "} 可以只留在环境变量里"})
			}
		}
		for _, k := range sortedKeys(e.Headers) {
			if literalSecret(k, e.Headers[k]) {
				out = append(out, Risk{Server: e.Name, Kind: "secret", Field: "headers." + k,
					Detail: "明文写进配置文件；改成 ${" + envVarName(k) + "} 可以只留在环境变量里"})
			}
		}
	}
	return out
}

// literalSecret reports a secret-looking value that is spelled out rather than
// referenced. ${VAR} and $VAR are references and carry no literal.
func literalSecret(key, value string) bool {
	v := strings.TrimSpace(value)
	if v == "" || strings.HasPrefix(v, "${") || strings.HasPrefix(v, "$") {
		return false
	}
	return SensitiveKey(key) || SensitiveValue(v)
}

// envVarName suggests the variable a literal should move into.
func envVarName(key string) string {
	up := strings.ToUpper(strings.TrimSpace(key))
	var b strings.Builder
	for _, r := range up {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	name := strings.Trim(b.String(), "_")
	if name == "" {
		return "SECRET"
	}
	return name
}
