package plugin

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/mcplaunch"
	"reasonix/internal/secrets"
	"reasonix/internal/tool"
)

// projectLaunchIdentityDigest resolves a secret-free identity before a project
// server is authorized. For stdio this pins the real executable path and file
// content; for HTTP it normalizes the endpoint while retaining only header key
// names. Installed and host-session servers never call this path.
func projectLaunchIdentityDigest(ctx context.Context, s Spec) (string, error) {
	identity, err := buildProjectLaunchIdentity(ctx, s)
	if err != nil {
		return "", err
	}
	return mcplaunch.ProjectLaunchIdentityDigest(identity)
}

func buildProjectLaunchIdentity(ctx context.Context, s Spec) (mcplaunch.ProjectLaunchIdentity, error) {
	transport := strings.ToLower(strings.TrimSpace(s.Type))
	if transport == "" {
		transport = "stdio"
	}
	launchArgs := effectiveLaunchArgs(s)
	if s.LauncherIdentityArgs != nil {
		launchArgs = s.LauncherIdentityArgs
	}
	identity := mcplaunch.ProjectLaunchIdentity{
		Server: s.Name, Transport: transport,
		Dir: s.Dir, Args: append([]string(nil), launchArgs...),
		EnvKeys: sortedMapKeys(s.Env), HeaderKeys: sortedMapKeys(s.Headers),
		LauncherDigest: s.LauncherDigest,
	}
	switch transport {
	case "stdio":
		identity.Dir = stdioWorkingDir(s)
		if strings.TrimSpace(s.Command) == "" {
			return mcplaunch.ProjectLaunchIdentity{}, fmt.Errorf("stdio plugin %q: command is required", s.Name)
		}
		env := mergeEnv(secrets.ProcessEnv(), s.Env)
		exe, _, err := resolveStdioExecutable(ctx, s, env)
		if err != nil {
			return mcplaunch.ProjectLaunchIdentity{}, err
		}
		identity.CommandPath = exe
		identity.CommandSHA256, err = mcplaunch.FileSHA256(exe)
		if err != nil {
			return mcplaunch.ProjectLaunchIdentity{}, fmt.Errorf("hash MCP executable %q: %w", exe, err)
		}
	case "http", "streamable-http", "streamable_http":
		identity.Transport = "http"
		identity.URL = normalizeIdentityURL(s.URL)
	default:
		identity.URL = normalizeIdentityURL(s.URL)
	}
	return identity, nil
}

// MCPStateDir returns a stable, server-scoped host directory outside the
// workspace for state that must survive across calls and sessions.
func MCPStateDir(reasonixHome, workspace, server string) string {
	if strings.TrimSpace(reasonixHome) == "" {
		return ""
	}
	workspaceID := mcplaunch.WorkspaceFingerprint(workspace)
	if len(workspaceID) > 16 {
		workspaceID = workspaceID[:16]
	}
	if workspaceID == "" {
		workspaceID = "global"
	}
	return filepath.Join(reasonixHome, "mcp-state", workspaceID, slug(server))
}

// identityURLRedacted replaces credential material inside identity and cache
// URLs. Only the structure survives: whether userinfo/a password exists and
// how many values a credential parameter carries, never their contents.
const identityURLRedacted = "__redacted__"

// credentialURLQueryKeys lists query parameters whose values are credentials.
// Keys are compared case-insensitively after removing "-" and "_", so
// api_key, api-key, x-api-key, and APIKEY normalize consistently, and any
// normalized key ending in a credentialURLQuerySuffixes entry (auth_token,
// refresh_token, id_token, client_secret, sas_signature, ...) is a credential
// too. Non-sensitive parameters (workspace, tenant, region, resource, ...)
// keep their values so a resource scope change still re-triggers verification.
var credentialURLQueryKeys = map[string]bool{
	"auth": true, "authorization": true, "bearer": true, "credential": true,
	"credentials": true, "sig": true,
	// The key family stays an exact list: a bare "*key" suffix would also
	// swallow unrelated words (monkey, sortkey-like resource names).
	"key": true, "accesskey": true, "secretkey": true, "privatekey": true,
	"authkey": true, "appkey": true, "clientkey": true, "subscriptionkey": true,
	"sharedkey": true,
}

// credentialURLQuerySuffixes classifies whole credential families by suffix:
// every *token, *secret, *password/*passwd, *apikey, and *signature parameter
// carries a credential value regardless of its prefix.
var credentialURLQuerySuffixes = []string{
	"token", "secret", "password", "passwd", "apikey", "signature",
}

func credentialURLQueryKey(key string) bool {
	normalized := strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	if credentialURLQueryKeys[normalized] {
		return true
	}
	for _, suffix := range credentialURLQuerySuffixes {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

// normalizeIdentityURL canonicalizes an MCP endpoint for host-local identity
// and schema-cache keys: scheme/host case and default ports fold,
// the fragment drops, query keys sort stably, and credential material
// (userinfo, credential query values) is replaced by a fixed placeholder so
// rotation never invalidates an exact project launch authorization.
// Network requests always use the raw configured URL, never this form.
func normalizeIdentityURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimSpace(raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host = net.JoinHostPort(strings.Trim(host, "[]"), port)
	}
	u.Host = host
	u.Fragment = ""
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(identityURLRedacted, identityURLRedacted)
		} else {
			u.User = url.User(identityURLRedacted)
		}
	}
	if u.RawQuery != "" {
		query := u.Query()
		for key, values := range query {
			if credentialURLQueryKey(key) {
				for i := range values {
					values[i] = identityURLRedacted
				}
			} else {
				sort.Strings(values)
			}
			query[key] = values
		}
		// Encode sorts keys, so equivalent URLs cannot differ by parameter order.
		u.RawQuery = query.Encode()
	}
	return u.String()
}

// legacyNormalizeIdentityURL is the pre-credential-aware normalization kept
// only so old schema-cache keys remain readable during the compatibility
// window. It no longer participates in authorization or tool classification.
func legacyNormalizeIdentityURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimSpace(raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host = net.JoinHostPort(strings.Trim(host, "[]"), port)
	}
	u.Host = host
	u.Fragment = ""
	return u.String()
}

func sortedMapKeys[V any](values map[string]V) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		if key = strings.TrimSpace(key); key != "" {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func launchConfigSource(s Spec) string {
	return s.ConfigSource
}

// CachedToolSafety is the local safety classification for one tool in an
// identity-matched schema cache. Authorization belongs to the MCP server;
// cached tools retain only safety facts that must match the live server.
type CachedToolSafety struct {
	ReadOnly    bool
	Destructive bool
}

// LiveToolSafety returns the host-local execution classification for a live MCP
// target. remoteTool uses one locked snapshot; compatibility adapters fall back
// to the public tool interfaces. The result never changes provider-visible
// schemas.
func LiveToolSafety(target tool.Tool) CachedToolSafety {
	if target == nil {
		return CachedToolSafety{}
	}
	if remote, ok := target.(*remoteTool); ok {
		_, readOnly, destructive := remote.securitySnapshot()
		return CachedToolSafety{
			ReadOnly:    readOnly,
			Destructive: destructive,
		}
	}
	safety := CachedToolSafety{ReadOnly: target.ReadOnly()}
	if annotations, ok := target.(tool.MCPAnnotations); ok {
		safety.Destructive = annotations.MCPDestructiveHint()
	}
	return safety
}

// ReconcileCachedToolSafety is the single cached-to-live boundary shared by
// lazy and on-demand MCP adapters. Both adapters stop the current call when the
// live server is stricter than the snapshot, before the direct tool executes.
func ReconcileCachedToolSafety(server, rawName string, cached CachedToolSafety, target tool.Tool) (CachedToolSafety, error) {
	live := LiveToolSafety(target)
	if target == nil {
		return live, nil
	}
	if cached.ReadOnly && !live.ReadOnly {
		return live, fmt.Errorf("MCP server %q no longer marks tool %q as read-only; the current call was blocked before execution — retry so Reasonix can apply the current Plan/read-only safety boundary", server, rawName)
	}
	if !cached.Destructive && live.Destructive {
		return live, fmt.Errorf("MCP server %q now marks tool %q as destructive; retry so Reasonix can apply the current Plan/read-only safety boundary before execution", server, rawName)
	}
	return live, nil
}

func CachedToolSafetyForSpec(s Spec, rawName string) (CachedToolSafety, bool) {
	cs, ok := LoadCachedSchemaForSpec(s)
	if !ok {
		return CachedToolSafety{}, false
	}
	var target *CachedToolSafety
	for _, cached := range cs.Tools {
		if cached.Name == rawName {
			copy := CachedToolSafety{ReadOnly: cached.ReadOnly, Destructive: cached.Destructive}
			target = &copy
			break
		}
	}
	if target == nil {
		return CachedToolSafety{}, false
	}
	return *target, true
}
