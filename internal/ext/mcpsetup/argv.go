package mcpsetup

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/config"
)

// ParseArgs reads the argv forms `mcp add` accepts:
//
//	-- npx -y chrome-devtools-mcp@latest      (name derived from the package)
//	https://example.com/mcp                   (name derived from the host)
//	<name> [command...|--http URL] [--env K=V] [--header K=V]
func ParseArgs(args []string) (config.PluginEntry, error) {
	var e config.PluginEntry
	if len(args) == 0 {
		return e, fmt.Errorf("mcp add: missing server name, command, or URL")
	}

	if args[0] == "--" {
		if len(args) < 2 {
			return e, fmt.Errorf("mcp add: -- requires a command argv")
		}
		e.Command = args[1]
		e.Args = append([]string(nil), args[2:]...)
		e.Name = NameFromArgv(e.Command, e.Args)
		if e.Name == "" {
			return e, fmt.Errorf("mcp add: could not derive a server name from the command; pass an explicit name")
		}
		return e, nil
	}
	if looksLikeRemoteURL(args[0]) && (len(args) == 1 || strings.HasPrefix(args[1], "-")) {
		e.Name = NameFromURL(args[0])
		e.Type, e.URL = "http", args[0]
		// Allow trailing --header/--env after a bare URL.
		if len(args) > 1 {
			restEntry, err := ParseArgs(append([]string{e.Name, "--http", args[0]}, args[1:]...))
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
		if len(rest) < 2 {
			return e, fmt.Errorf("mcp add: -- requires a command argv")
		}
		e.Command = rest[1]
		e.Args = append([]string(nil), rest[2:]...)
		return e, nil
	}
	if err := applyFlags(&e, rest); err != nil {
		return e, err
	}
	switch {
	case e.URL != "" && e.Command != "":
		return e, fmt.Errorf("mcp add: specify a command OR a --http/--sse URL, not both")
	case e.URL == "" && e.Command == "":
		return e, fmt.Errorf("mcp add: need a command (stdio) or a --http/--sse URL")
	}
	return e, nil
}

// applyFlags reads the flags after the server name. The first non-flag token
// ends the flag section and begins the stdio command, whose own -flags are its
// argv and must not be parsed here (e.g. `npx -y pkg`).
func applyFlags(e *config.PluginEntry, rest []string) error {
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if !strings.HasPrefix(a, "-") {
			e.Command = a
			e.Args = append([]string(nil), rest[i+1:]...)
			return nil
		}
		key, value, hasInline := strings.Cut(a, "=")
		if !hasInline {
			if i+1 >= len(rest) {
				return fmt.Errorf("mcp add: %s needs a value", key)
			}
			i++
			value = rest[i]
		}
		switch key {
		case "--http", "--streamable-http":
			e.Type, e.URL = "http", value
		case "--sse":
			e.Type, e.URL = "sse", value
		case "--env":
			if err := putPair(&e.Env, key, value); err != nil {
				return err
			}
		case "--header":
			if err := putPair(&e.Headers, key, value); err != nil {
				return err
			}
		default:
			return fmt.Errorf("mcp add: unknown flag %q", a)
		}
	}
	return nil
}

func putPair(dst *map[string]string, flag, pair string) error {
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

func looksLikeRemoteURL(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
}

// NameFromURL derives a server name from a hosted MCP endpoint.
func NameFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "remote-mcp"
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	host = strings.Split(host, ".")[0]
	host = SanitizeName(host)
	if host == "" {
		return "remote-mcp"
	}
	return host
}

// NameFromArgv derives a server name from the command that starts it, looking
// through the runner (npx/uvx/python -m/…) to the package it actually launches.
func NameFromArgv(command string, args []string) string {
	runner := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(filepath.Base(strings.ReplaceAll(command, "\\", "/"))), ".exe"), ".cmd"), ".bat")
	candidate := command
	launcher, known := commandLaunchers[runner]
	if known {
		candidate = launcherCommandOperand(args, launcher)
		if candidate == "" {
			return "mcp-server"
		}
	}
	base := filepath.Base(strings.ReplaceAll(candidate, "\\", "/"))
	if known && launcher.image {
		base, _, _ = strings.Cut(base, ":")
	}
	if runner == "uvx" {
		base, _, _ = strings.Cut(base, "==")
	}
	if at := strings.Index(base, "@"); at > 0 {
		base = base[:at]
	}
	for _, ext := range []string{".js", ".exe", ".cmd", ".bat"} {
		base = strings.TrimSuffix(base, ext)
	}
	name := SanitizeName(base)
	if name == "" {
		return "mcp-server"
	}
	return name
}

// SanitizeName reduces a derived name to the lowercase-and-dashes form config
// and tool prefixes accept.
func SanitizeName(raw string) string {
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

// Tokenize splits a command line into arguments, honouring "double" and 'single'
// quotes so values with spaces (e.g. --header "Authorization=Bearer x") survive.
// An unterminated quote takes the rest of the line as one token.
func Tokenize(s string) []string {
	var out []string
	var cur strings.Builder
	inWord := false
	var quote rune
	chars := []rune(s)
	for i := 0; i < len(chars); i++ {
		r := chars[i]
		if r == '\\' && quote != '\'' && i+1 < len(chars) && chars[i+1] == '\n' {
			start := i
			for start > 0 && chars[start-1] == '\\' {
				start--
			}
			if (i-start)%2 == 0 {
				i++
				continue
			}
		}
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
