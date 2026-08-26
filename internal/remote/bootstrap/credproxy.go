package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/remote/sftpfs"
)

// TokenEnvName is the remote .env entry read by the installed provider.
const TokenEnvName = "REASONIX_PROXY_TOKEN"

const managedProviderComment = "# managed by the Reasonix desktop credential proxy — safe to delete"

// isRemoteMissing reports whether err is the SFTP "no such file" condition
// (pkg/sftp maps it onto os.ErrNotExist; the text match covers older wraps).
func isRemoteMissing(err error) bool {
	return err != nil && (errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file"))
}

// remoteConfigPath is ~/.reasonix/config.toml on the remote host.
func remoteConfigPath(home string) string {
	return path.Join(home, ".reasonix", "config.toml")
}

// tomlString renders s as a basic TOML string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// credentialProxyKind normalizes the options' provider kind.
func credentialProxyKind(opts *CredentialProxyOptions) string {
	kind := strings.TrimSpace(opts.Kind)
	if kind == "" {
		kind = "openai"
	}
	return kind
}

func credentialProxyTokenEnv(opts *CredentialProxyOptions) string {
	if name := strings.TrimSpace(opts.TokenEnv); name != "" {
		return name
	}
	return TokenEnvName
}

// credentialProviderBlock renders the tunnel-backed remote provider entry.
func credentialProviderBlock(opts *CredentialProxyOptions) string {
	var b strings.Builder
	b.WriteString("\n[[providers]]\n")
	b.WriteString(managedProviderComment + "\n")
	b.WriteString("name = " + tomlString(opts.Provider) + "\n")
	b.WriteString("kind = " + tomlString(credentialProxyKind(opts)) + "\n")
	b.WriteString("base_url = " + tomlString(opts.BaseURL) + "\n")
	b.WriteString("model = " + tomlString(opts.Model) + "\n")
	b.WriteString("api_key_env = " + tomlString(credentialProxyTokenEnv(opts)) + "\n")
	return b.String()
}

// CredentialProxyOptions configures local-proxy credential mode: the remote
// serve's model calls route back to the desktop over the SSH reverse tunnel,
// so the real provider key never leaves the desktop.
type CredentialProxyOptions struct {
	// BaseURL is the loopback URL on the REMOTE host that tunnels back to the
	// desktop's credential proxy, e.g. http://127.0.0.1:18999.
	BaseURL string
	// Token is the scoped virtual token stored in the remote 0600 global .env.
	Token string
	// TokenEnv is its workspace-specific environment variable name.
	TokenEnv string
	// Provider is the provider name installed into the remote config; the
	// serve is launched with --model <Provider> so it selects this entry.
	Provider string
	// Model is the model name the provider entry carries (the desktop's
	// current default model, resolved by the caller).
	Model string
	// Kind is the provider kind the entry carries ("openai" or "anthropic"):
	// the serve formats its model requests per kind, so it must match the
	// desktop provider behind the proxy. Empty reads as "openai".
	Kind string
}

// EnsureCredentialProvider updates only the managed tunnel-backed provider and
// virtual credential on an already connected host. Desktop model switches use
// this without restarting Serve; the controller adopts the staged provider via
// its ordinary active-work-gated model switch.
func EnsureCredentialProvider(ctx context.Context, conn Conn, opts *CredentialProxyOptions) (bool, error) {
	if conn == nil {
		return false, fmt.Errorf("bootstrap: remote connection is required")
	}
	fs, err := conn.SFTP()
	if err != nil {
		return false, err
	}
	home, err := fs.RealPath(ctx, "~")
	if err != nil {
		return false, fmt.Errorf("bootstrap: resolve remote home: %w", err)
	}
	return ensureCredentialProvider(ctx, fs, home, opts)
}

// ensureCredentialProvider installs or heals the proxy provider and virtual
// token. The result reports whether a running serve must reload its config.
func ensureCredentialProvider(ctx context.Context, fs *sftpfs.FS, home string, opts *CredentialProxyOptions) (bool, error) {
	if opts == nil || strings.TrimSpace(opts.BaseURL) == "" || strings.TrimSpace(opts.Token) == "" ||
		strings.TrimSpace(opts.Provider) == "" || strings.TrimSpace(opts.Model) == "" {
		return false, fmt.Errorf("bootstrap: credential proxy options are incomplete")
	}
	tokenEnv := credentialProxyTokenEnv(opts)
	if !config.IsValidCredentialKey(tokenEnv) {
		return false, fmt.Errorf("bootstrap: credential proxy token env %q is invalid", tokenEnv)
	}
	kind := credentialProxyKind(opts)
	cfgPath := remoteConfigPath(home)
	data, _, _, rerr := fs.ReadFile(ctx, cfgPath, 1<<20)
	if rerr != nil && !isRemoteMissing(rerr) {
		return false, fmt.Errorf("bootstrap: read remote config: %w", rerr)
	}
	original := string(data)
	// An explicit providers table replaces built-ins, so materialize a built-in
	// default before appending ours without rewriting default_model itself.
	existing := materializeDefaultProvider(original)
	existing, _ = rewriteManagedProviderBaseURLs(existing, opts.BaseURL)
	configChanged := existing != original
	if idx := providerBlockIndex(existing, opts.Provider); idx >= 0 {
		if providerBlockHasBaseURL(existing[idx:], opts.BaseURL) && providerBlockHasKind(existing[idx:], kind) && providerBlockHasModel(existing[idx:], opts.Model) {
			// Config is already current, but the .env token is healed
			// independently — an unchanged base_url must not skip it.
			envChanged, err := ensureCredentialToken(ctx, fs, home, tokenEnv, opts.Token)
			if err != nil {
				return false, err
			}
			if !configChanged {
				return envChanged, nil
			}
			if err := fs.MkdirAll(ctx, path.Dir(cfgPath)); err != nil {
				return false, err
			}
			if err := fs.WriteFileAtomic(ctx, cfgPath, []byte(existing), 0o600); err != nil {
				return false, err
			}
			return true, nil
		}
		if !providerBlockHasBaseURL(existing[idx:], opts.BaseURL) {
			updated, ok := replaceProviderBaseURL(existing, idx, opts.BaseURL)
			if !ok {
				return false, fmt.Errorf("bootstrap: remote config provider %q needs a manual base_url update", opts.Provider)
			}
			existing = updated
		}
		if !providerBlockHasKind(existing[idx:], kind) {
			updated, ok := replaceProviderKind(existing, idx, kind)
			if !ok {
				return false, fmt.Errorf("bootstrap: remote config provider %q needs a manual kind update", opts.Provider)
			}
			existing = updated
		}
		if !providerBlockHasModel(existing[idx:], opts.Model) {
			updated, ok := replaceProviderModel(existing, idx, opts.Model)
			if !ok {
				return false, fmt.Errorf("bootstrap: remote config provider %q needs a manual model update", opts.Provider)
			}
			existing = updated
		}
	} else {
		existing += credentialProviderBlock(opts)
	}
	if err := fs.MkdirAll(ctx, path.Dir(cfgPath)); err != nil {
		return false, err
	}
	if err := fs.WriteFileAtomic(ctx, cfgPath, []byte(existing), 0o600); err != nil {
		return false, err
	}
	// Runtime credential resolution reads the global .env file.
	if _, err := ensureCredentialToken(ctx, fs, home, tokenEnv, opts.Token); err != nil {
		return false, err
	}
	return true, nil
}

// rewriteManagedProviderBaseURLs heals every workspace provider that this
// desktop installed. All of them share the host's one reverse-forward port,
// which changes together after an SSH reconnect.
func rewriteManagedProviderBaseURLs(text, baseURL string) (string, bool) {
	lines := strings.Split(text, "\n")
	inProvider, managed, changed := false, false, false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inProvider = trimmed == "[[providers]]"
			managed = false
			continue
		}
		if !inProvider {
			continue
		}
		if trimmed == managedProviderComment {
			managed = true
			continue
		}
		if managed && strings.HasPrefix(trimmed, "base_url") && strings.Contains(trimmed, "=") {
			want := "base_url = " + tomlString(baseURL)
			if trimmed != want {
				indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
				lines[index] = indent + want
				changed = true
			}
		}
	}
	if !changed {
		return text, false
	}
	return strings.Join(lines, "\n"), true
}

// ensureCredentialToken idempotently writes the credential-proxy token into
// the remote global .env, preserving every other line. Reports whether the
// value was written or already current.
func ensureCredentialToken(ctx context.Context, fs *sftpfs.FS, home, envName, token string) (bool, error) {
	envPath := path.Join(home, ".reasonix", ".env")
	data, _, _, rerr := fs.ReadFile(ctx, envPath, 1<<20)
	if rerr != nil && !isRemoteMissing(rerr) {
		return false, fmt.Errorf("bootstrap: read remote .env: %w", rerr)
	}
	lines := strings.Split(string(data), "\n")
	prefix := envName + "="
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			if strings.TrimSpace(line) == prefix+token {
				return false, nil
			}
			lines[i] = prefix + token
			updated := strings.Join(lines, "\n")
			return true, fs.WriteFileAtomic(ctx, envPath, []byte(updated), 0o600)
		}
	}
	// Append (creating the file when missing). Keep the trailing-newline
	// convention so later manual edits stay clean.
	content := string(data)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += prefix + token + "\n"
	return true, fs.WriteFileAtomic(ctx, envPath, []byte(content), 0o600)
}

// providerBlockIndex finds the start of the [[providers]] block whose name
// equals provider, or -1. Blocks are scanned line-wise; a block ends at the
// next table header.
func providerBlockIndex(text, provider string) int {
	want := "name = " + tomlString(provider)
	lines := strings.Split(text, "\n")
	offset := 0
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[[") || strings.HasPrefix(trimmed, "[") {
			inBlock = strings.HasPrefix(trimmed, "[[providers]]")
		} else if inBlock && trimmed == want {
			return offset
		}
		offset += len(line) + 1
	}
	return -1
}

// providerBlockHasBaseURL reports whether the block starting at idx contains
// the given base_url assignment before its next table header.
func providerBlockHasBaseURL(block, baseURL string) bool {
	want := "base_url = " + tomlString(baseURL)
	for line := range strings.SplitSeq(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			return false
		}
		if trimmed == want {
			return true
		}
	}
	return false
}

// replaceProviderBaseURL swaps the base_url line inside the block starting at
// idx, preserving everything else byte-for-byte.
func replaceProviderBaseURL(text string, idx int, baseURL string) (string, bool) {
	rest := text[idx:]
	lines := strings.Split(rest, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i > 0 && strings.HasPrefix(trimmed, "[") {
			break
		}
		if strings.HasPrefix(trimmed, "base_url") && strings.Contains(trimmed, "=") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "base_url = " + tomlString(baseURL)
			return text[:idx] + strings.Join(lines, "\n"), true
		}
	}
	return text, false
}

// providerBlockHasKind reports whether the block starting at idx contains the
// given kind assignment before its next table header.
func providerBlockHasKind(block, kind string) bool {
	want := "kind = " + tomlString(kind)
	for line := range strings.SplitSeq(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			return false
		}
		if trimmed == want {
			return true
		}
	}
	return false
}

// replaceProviderKind swaps the kind line inside the block starting at idx,
// preserving everything else byte-for-byte.
func replaceProviderKind(text string, idx int, kind string) (string, bool) {
	rest := text[idx:]
	lines := strings.Split(rest, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i > 0 && strings.HasPrefix(trimmed, "[") {
			break
		}
		if strings.HasPrefix(trimmed, "kind") && strings.Contains(trimmed, "=") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "kind = " + tomlString(kind)
			return text[:idx] + strings.Join(lines, "\n"), true
		}
	}
	return text, false
}

func providerBlockHasModel(block, model string) bool {
	want := "model = " + tomlString(model)
	for line := range strings.SplitSeq(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			return false
		}
		if trimmed == want {
			return true
		}
	}
	return false
}

func replaceProviderModel(text string, idx int, model string) (string, bool) {
	rest := text[idx:]
	lines := strings.Split(rest, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i > 0 && strings.HasPrefix(trimmed, "[") {
			break
		}
		if strings.HasPrefix(trimmed, "model") && strings.Contains(trimmed, "=") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + "model = " + tomlString(model)
			return text[:idx] + strings.Join(lines, "\n"), true
		}
	}
	return text, false
}

// materializeDefaultProvider appends an explicit [[providers]] entry for the
// provider the top-level default_model refers to when that provider currently
// resolves only through the built-in defaults. Returns the text unchanged
// when default_model is absent, already defined in the file, or not a
// builtin. default_model itself is never rewritten — the remote's model
// choice stays exactly as the user configured it.
func materializeDefaultProvider(existing string) string {
	name := defaultModelProvider(existing)
	if name == "" || providerBlockIndex(existing, name) >= 0 {
		return existing
	}
	entry, ok := config.BuiltinProviderEntry(name)
	if !ok {
		return existing
	}
	return existing + providerEntryBlock(entry)
}

// defaultModelProvider extracts the provider part of the top-level
// default_model assignment: "deepseek-flash" → "deepseek-flash",
// "deepseek/deepseek-v4-flash" → "deepseek". Empty when absent. Scanning
// stops at the first table header — default_model is only meaningful at the
// top of the file.
func defaultModelProvider(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			return ""
		}
		after, ok := strings.CutPrefix(trimmed, "default_model")
		if !ok || !strings.HasPrefix(strings.TrimSpace(after), "=") {
			continue
		}
		value := firstQuoted(after)
		if value == "" {
			return ""
		}
		provider, _, _ := strings.Cut(value, "/")
		return strings.TrimSpace(provider)
	}
	return ""
}

// firstQuoted returns the first double-quoted substring of s, skipping a
// trailing inline comment.
func firstQuoted(s string) string {
	i := strings.Index(s, `"`)
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i+1:], `"`)
	if j < 0 {
		return ""
	}
	return s[i+1 : i+1+j]
}

// providerEntryBlock renders a builtin ProviderEntry as a TOML block with the
// connection fields the serve needs (name/kind/base_url/model/api_key_env,
// plus the models list form); secrets stay in api_key_env as everywhere else.
func providerEntryBlock(p config.ProviderEntry) string {
	var b strings.Builder
	b.WriteString("\n[[providers]]\n")
	b.WriteString("# materialized from the built-in defaults by the desktop credential proxy — safe to delete\n")
	fmt.Fprintf(&b, "name = %s\n", tomlString(p.Name))
	fmt.Fprintf(&b, "kind = %s\n", tomlString(p.Kind))
	fmt.Fprintf(&b, "base_url = %s\n", tomlString(p.BaseURL))
	if p.Model != "" {
		fmt.Fprintf(&b, "model = %s\n", tomlString(p.Model))
	}
	if len(p.Models) > 0 {
		quoted := make([]string, len(p.Models))
		for i, m := range p.Models {
			quoted[i] = tomlString(m)
		}
		fmt.Fprintf(&b, "models = [%s]\n", strings.Join(quoted, ", "))
		if p.Default != "" {
			fmt.Fprintf(&b, "default = %s\n", tomlString(p.Default))
		}
	}
	if p.APIKeyEnv != "" {
		fmt.Fprintf(&b, "api_key_env = %s\n", tomlString(p.APIKeyEnv))
	}
	if p.BalanceURL != "" {
		fmt.Fprintf(&b, "balance_url = %s\n", tomlString(p.BalanceURL))
	}
	return b.String()
}
