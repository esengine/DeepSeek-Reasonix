package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/mcpcatalog"
	"reasonix/internal/mcplaunch"
	"reasonix/internal/sandbox"
	"reasonix/internal/secrets"
)

// specIdentityFingerprint resolves a secret-free identity before a server is
// trusted. For stdio this pins the real executable path and file content; for
// HTTP it normalizes the endpoint while retaining only header key names.
func specIdentityFingerprint(ctx context.Context, s Spec) (string, error) {
	identity, err := buildSpecIdentity(ctx, s)
	if err != nil {
		return "", err
	}
	return mcplaunch.IdentityFingerprint(identity)
}

func buildSpecIdentity(ctx context.Context, s Spec) (mcplaunch.Identity, error) {
	transport := strings.ToLower(strings.TrimSpace(s.Type))
	if transport == "" {
		transport = "stdio"
	}
	if strings.TrimSpace(s.OfficialCatalogEntryID) != "" {
		if err := validateOfficialLauncher(s); err != nil {
			return mcplaunch.Identity{}, err
		}
		if strings.TrimSpace(s.PackageRoot) == "" || strings.TrimSpace(s.PackageDigest) == "" {
			return mcplaunch.Identity{}, fmt.Errorf("official MCP server %q is missing its verified package root or digest", s.Name)
		}
		liveDigest, err := mcpcatalog.TreeSHA256(s.PackageRoot)
		if err != nil {
			return mcplaunch.Identity{}, fmt.Errorf("verify official MCP package for %q: %w", s.Name, err)
		}
		if !strings.EqualFold(liveDigest, s.PackageDigest) {
			return mcplaunch.Identity{}, fmt.Errorf("official MCP package for %q changed after verification; blocked before process or network startup", s.Name)
		}
	}
	launchArgs := effectiveLaunchArgs(s)
	if s.LauncherIdentityArgs != nil {
		launchArgs = s.LauncherIdentityArgs
	}
	identity := mcplaunch.Identity{
		Server: s.Name, Transport: transport, ConfigSource: s.ConfigSource,
		Dir: s.Dir, Args: append([]string(nil), launchArgs...),
		EnvKeys: sortedMapKeys(s.Env), HeaderKeys: sortedMapKeys(s.Headers),
		Network: s.ReaderSandbox.Network || s.WriterSandbox.Network,
		WriteRoots: append(append(append([]string(nil), s.ReaderSandbox.WriteRoots...),
			s.ReaderSandbox.AppContainerWriteRoots...), s.WriterSandbox.WriteRoots...),
		ReadRoots: append(append([]string(nil), s.ReaderSandbox.ReadRoots...), s.WriterSandbox.ReadRoots...),
		ForbidReadRoots: append(append([]string(nil), s.ReaderSandbox.ForbidReadRoots...),
			s.WriterSandbox.ForbidReadRoots...),
		IsolationPolicy: isolationPolicy(s),
		PackageDigest:   s.PackageDigest,
		LauncherDigest:  s.LauncherDigest,
	}
	if strings.TrimSpace(s.OfficialCatalogEntryID) != "" {
		// Verified official package identity is global across workspaces. The signed package digest
		// pins executable code and the catalog pins the server definition, so
		// workspace-expanded args and write-root paths are excluded here.
		identity.Args = nil
		identity.Dir = ""
		identity.WriteRoots = nil
		identity.ReadRoots = nil
		identity.ForbidReadRoots = nil
		identity.ConfigSource = "official_catalog:" + s.OfficialCatalogEntryID
	}
	switch transport {
	case "stdio":
		if strings.TrimSpace(s.Command) == "" {
			return mcplaunch.Identity{}, fmt.Errorf("stdio plugin %q: command is required", s.Name)
		}
		env := mergeEnv(secrets.ProcessEnv(), s.Env)
		exe, _, err := resolveStdioExecutable(ctx, s, env)
		if err != nil {
			return mcplaunch.Identity{}, err
		}
		if abs, err := filepath.Abs(exe); err == nil {
			exe = abs
		}
		identity.CommandPath = exe
		identity.CommandSHA256, err = mcplaunch.FileSHA256(exe)
		if err != nil {
			return mcplaunch.Identity{}, fmt.Errorf("hash MCP executable %q: %w", exe, err)
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

func isolationPolicy(s Spec) string {
	transport := strings.ToLower(strings.TrimSpace(s.Type))
	if transport == "http" || transport == "streamable-http" || transport == "streamable_http" {
		return "not_applicable"
	}
	if !s.ReaderSandbox.Enforce() && !s.WriterSandbox.Enforce() {
		return "off"
	}
	if sandbox.Available() {
		return "enforced"
	}
	return "unavailable_unconfined"
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
// and schema-cache fingerprints: scheme/host case and default ports fold,
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
	if s.OfficialCatalogEntryID != "" {
		return "official_catalog:" + s.OfficialCatalogEntryID
	}
	return s.ConfigSource
}

type toolCapability struct {
	RawName      string
	ModelName    string
	VisibleName  string
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
	ReadOnly     bool
	Destructive  bool
}

func capabilityOf(s Spec, raw mcpTool, schema []byte) toolCapability {
	visible := raw.Name
	if s.StripRawPrefix != "" {
		visible = strings.TrimPrefix(visible, s.StripRawPrefix)
	}
	hinted := raw.Annotations != nil && raw.Annotations.ReadOnlyHint
	destructive := raw.Annotations != nil && raw.Annotations.DestructiveHint
	return toolCapability{
		RawName: raw.Name, ModelName: toolName(s.Name, visible), VisibleName: visible,
		InputSchema: schema, OutputSchema: raw.OutputSchema,
		ReadOnly: hinted || s.toolReadOnlyOverride(raw.Name, visible), Destructive: destructive,
	}
}

func trustedReaderForSpec(s Spec, cap toolCapability) bool {
	if !cap.ReadOnly || cap.Destructive {
		return false
	}
	if s.toolReadOnlyOverride(cap.RawName, cap.VisibleName) {
		return true
	}
	if strings.TrimSpace(s.OfficialCatalogEntryID) == "" || mcpcatalog.RuntimeEntryRevoked(s.OfficialCatalogEntryID) {
		return false
	}
	for _, name := range s.OfficialReaderNames {
		if strings.TrimSpace(name) == cap.RawName {
			return true
		}
	}
	return false
}

func capabilityFingerprint(cap toolCapability) string {
	in, err := canonicalSecuritySchema(cap.InputSchema)
	if err != nil {
		return ""
	}
	out, err := canonicalSecuritySchema(cap.OutputSchema)
	if err != nil {
		return ""
	}
	payload := struct {
		RawName     string          `json:"raw_name"`
		ModelName   string          `json:"model_name"`
		Input       json.RawMessage `json:"input,omitempty"`
		Output      json.RawMessage `json:"output,omitempty"`
		ReadOnly    bool            `json:"read_only"`
		Destructive bool            `json:"destructive"`
	}{
		RawName: strings.TrimSpace(cap.RawName), ModelName: strings.TrimSpace(cap.ModelName),
		Input: in, Output: out, ReadOnly: cap.ReadOnly, Destructive: cap.Destructive,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func canonicalSecuritySchema(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	stripSchemaDisplayFields(value)
	body, err := json.Marshal(value)
	return json.RawMessage(body), err
}

func stripSchemaDisplayFields(value any) {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"description", "title", "examples", "$comment"} {
			delete(v, key)
		}
		for key, child := range v {
			switch key {
			case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas", "dependentRequired", "dependencies":
				if named, ok := child.(map[string]any); ok {
					for _, schema := range named {
						stripSchemaDisplayFields(schema)
					}
					continue
				}
			}
			stripSchemaDisplayFields(child)
		}
	case []any:
		for _, child := range v {
			stripSchemaDisplayFields(child)
		}
	}
}

// CachedToolSafety is the local safety classification for one tool in an
// identity-matched schema cache. Server hints control ordinary read-only
// policy; only explicit local declarations or signed catalog readers are
// admitted into strict read-only execution.
type CachedToolSafety struct {
	ReadOnly              bool
	TrustedReader         bool
	Destructive           bool
	CapabilityFingerprint string
}

func CachedToolSafetyForSpec(s Spec, rawName string) (CachedToolSafety, bool) {
	cs, ok := LoadCachedSchemaForSpec(s)
	if !ok {
		return CachedToolSafety{}, false
	}
	var target *toolCapability
	for _, cached := range cs.Tools {
		visible := cached.Name
		if s.StripRawPrefix != "" {
			visible = strings.TrimPrefix(visible, s.StripRawPrefix)
		}
		cap := toolCapability{
			RawName: cached.Name, ModelName: toolName(s.Name, visible), VisibleName: visible,
			InputSchema: cached.Schema, OutputSchema: cached.OutputSchema,
			ReadOnly:    cached.ReadOnly || s.toolReadOnlyOverride(cached.Name, visible),
			Destructive: cached.Destructive,
		}
		if cached.Name == rawName {
			copy := cap
			target = &copy
		}
	}
	if target == nil {
		return CachedToolSafety{}, false
	}
	return CachedToolSafety{
		ReadOnly:              target.ReadOnly,
		TrustedReader:         trustedReaderForSpec(s, *target),
		Destructive:           target.Destructive,
		CapabilityFingerprint: capabilityFingerprint(*target),
	}, true
}
