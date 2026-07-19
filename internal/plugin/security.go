package plugin

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/mcpcatalog"
	"reasonix/internal/mcptrust"
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
	return mcptrust.IdentityFingerprint(identity)
}

// legacySpecIdentityFingerprint computes the identity fingerprint this spec
// had before credential-aware URL normalization, or ("", false) when the two
// cannot differ. It exists only so MigrateIdentityFingerprint can upgrade
// pre-rollout receipts by digest comparison; remove together with
// legacyNormalizeIdentityURL (see docs/MIGRATING.md).
func legacySpecIdentityFingerprint(s Spec) (string, bool) {
	legacyURL := legacyNormalizeIdentityURL(s.URL)
	if strings.TrimSpace(s.URL) == "" || legacyURL == normalizeIdentityURL(s.URL) {
		return "", false
	}
	// URL-bearing transports never resolve a stdio executable, so the identity
	// build needs no live context here.
	identity, err := buildSpecIdentity(context.Background(), s)
	if err != nil || identity.URL == "" {
		return "", false
	}
	identity.URL = legacyURL
	fingerprints := mcptrust.LegacyIdentityFingerprints(identity)
	if len(fingerprints) == 0 {
		return "", false
	}
	return fingerprints[0], true
}

func legacySpecIdentityFingerprints(s Spec) []string {
	identity, err := buildSpecIdentity(context.Background(), s)
	if err != nil {
		return nil
	}
	out := mcptrust.LegacyIdentityFingerprints(identity)
	if legacyURL := legacyNormalizeIdentityURL(s.URL); identity.URL != "" && legacyURL != normalizeIdentityURL(s.URL) {
		identity.URL = legacyURL
		out = append(out, mcptrust.LegacyIdentityFingerprints(identity)...)
	}
	return compactFingerprints(out)
}

func compactFingerprints(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func buildSpecIdentity(ctx context.Context, s Spec) (mcptrust.Identity, error) {
	transport := strings.ToLower(strings.TrimSpace(s.Type))
	if transport == "" {
		transport = "stdio"
	}
	if strings.TrimSpace(s.OfficialCatalogEntryID) != "" {
		if err := validateOfficialLauncher(s); err != nil {
			return mcptrust.Identity{}, err
		}
		if strings.TrimSpace(s.PackageRoot) == "" || strings.TrimSpace(s.PackageDigest) == "" {
			return mcptrust.Identity{}, fmt.Errorf("official MCP server %q is missing its verified package root or digest", s.Name)
		}
		liveDigest, err := mcpcatalog.TreeSHA256(s.PackageRoot)
		if err != nil {
			return mcptrust.Identity{}, fmt.Errorf("reverify official MCP package for %q: %w", s.Name, err)
		}
		if !strings.EqualFold(liveDigest, s.PackageDigest) {
			return mcptrust.Identity{}, fmt.Errorf("official MCP package for %q changed after verification; blocked before process or network startup", s.Name)
		}
	}
	identity := mcptrust.Identity{
		Server: s.Name, Transport: transport, ConfigSource: s.ConfigSource,
		Dir: s.Dir, Args: append([]string(nil), effectiveLaunchArgs(s)...),
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
		// Official trust is global across workspaces. The signed package digest
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
			return mcptrust.Identity{}, fmt.Errorf("stdio plugin %q: command is required", s.Name)
		}
		env := mergeEnv(secrets.ProcessEnv(), s.Env)
		exe, _, err := resolveStdioExecutable(ctx, s, env)
		if err != nil {
			return mcptrust.Identity{}, err
		}
		if abs, err := filepath.Abs(exe); err == nil {
			exe = abs
		}
		identity.CommandPath = exe
		identity.CommandSHA256, err = mcptrust.FileSHA256(exe)
		if err != nil {
			return mcptrust.Identity{}, fmt.Errorf("hash MCP executable %q: %w", exe, err)
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
	workspaceID := mcptrust.WorkspaceFingerprint(workspace)
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
		return string(mcptrust.IsolationNotApplicable)
	}
	if !s.ReaderSandbox.Enforce() && !s.WriterSandbox.Enforce() {
		return "off"
	}
	if sandbox.Available() {
		return string(mcptrust.IsolationEnforced)
	}
	return string(mcptrust.IsolationUnavailableUnconfined)
}

func isolationStateForSpec(s Spec) mcptrust.IsolationState {
	switch isolationPolicy(s) {
	case string(mcptrust.IsolationEnforced):
		return mcptrust.IsolationEnforced
	case string(mcptrust.IsolationUnavailableUnconfined):
		return mcptrust.IsolationUnavailableUnconfined
	default:
		return mcptrust.IsolationNotApplicable
	}
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
// rotation never invalidates trust and receipts never bind secret values.
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

// legacyNormalizeIdentityURL is the pre-credential-aware normalization, kept
// read-only so receipts and schema caches written before the rollout migrate
// by digest comparison instead of forcing a re-trust. Scheduled for removal
// two minor releases after the rollout (see docs/MIGRATING.md).
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

func validatePersistentTransportTrust(s Spec) error {
	transport := strings.ToLower(strings.TrimSpace(s.Type))
	switch transport {
	case "http", "streamable-http", "streamable_http", "sse":
		u, err := url.Parse(strings.TrimSpace(s.URL))
		if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
			return fmt.Errorf("persistent trust for remote MCP server %q requires an HTTPS URL; use session trust for this connection", s.Name)
		}
	}
	return nil
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

func capabilityOf(s Spec, raw mcpTool, schema []byte) mcptrust.Capability {
	visible := raw.Name
	if s.StripRawPrefix != "" {
		visible = strings.TrimPrefix(visible, s.StripRawPrefix)
	}
	hinted := raw.Annotations != nil && raw.Annotations.ReadOnlyHint
	destructive := raw.Annotations != nil && raw.Annotations.DestructiveHint
	return mcptrust.Capability{
		RawName: raw.Name, ModelName: toolName(s.Name, visible),
		InputSchema: schema, OutputSchema: raw.OutputSchema,
		ReadOnly:    hinted || s.toolReadOnlyOverride(raw.Name, visible),
		Destructive: destructive,
	}
}

// managerEvaluate is the shared evaluation entry for every path that can
// execute MCP tools. Official catalog servers evaluate against the current
// signed reader allowlist, and a receipt still carrying the legacy URL
// fingerprint upgrades in place when nothing else drifted.
func managerEvaluate(manager *mcptrust.Manager, s Spec, identity string, capabilities []mcptrust.Capability) (mcptrust.Evaluation, error) {
	eval, err := managerEvaluateOnce(manager, s, identity, capabilities)
	if err != nil {
		return eval, err
	}
	migrated, migErr := migrateLegacyIdentity(s, identity, capabilities)
	if migErr != nil {
		return eval, migErr
	}
	if !migrated {
		return eval, nil
	}
	return managerEvaluateOnce(manager, s, identity, capabilities)
}

// migrateLegacyIdentity upgrades a receipt still carrying the legacy URL
// fingerprint of this exact spec to the current credential-aware fingerprint.
// It runs before the pre-start identity block, where no live capabilities
// exist yet, so the drift comparison is deliberately skipped (nil): the
// receipt's tool snapshot is untouched and the post-handshake evaluation
// still performs the full capability drift check against it.
func migrateLegacyIdentity(s Spec, identity string, capabilities []mcptrust.Capability) (bool, error) {
	if s.TrustManager == nil {
		return false, nil
	}
	legacy := legacySpecIdentityFingerprints(s)
	if len(legacy) == 0 {
		return false, nil
	}
	currentSource := trustConfigSource(s)
	return s.TrustManager.MigrateIdentityFingerprints(
		s.Name,
		currentSource,
		[]string{currentSource, "workspace_config"},
		legacy,
		identity,
		capabilities,
	)
}

func managerEvaluateOnce(manager *mcptrust.Manager, s Spec, identity string, capabilities []mcptrust.Capability) (mcptrust.Evaluation, error) {
	if strings.TrimSpace(s.OfficialCatalogEntryID) != "" {
		return manager.EvaluateOfficial(s.Name, trustConfigSource(s), identity, capabilities, mcptrust.OfficialAuthority{
			CatalogEntryID: s.OfficialCatalogEntryID,
			Readers:        s.OfficialReaderNames,
		})
	}
	return manager.Evaluate(s.Name, trustConfigSource(s), identity, capabilities)
}

func evaluateSpecTrust(s Spec, identity string, capabilities []mcptrust.Capability) (mcptrust.Evaluation, error) {
	manager := s.TrustManager
	if manager == nil {
		trusted := map[string]bool{}
		for _, cap := range capabilities {
			// Direct plugin-package users without a composition-root manager keep
			// the historical annotation behavior. Every product frontend injects
			// a manager, where third-party hints require a receipt.
			if cap.ReadOnly {
				trusted[cap.RawName] = true
			}
		}
		return mcptrust.Evaluation{State: mcptrust.TrustUntrusted, TrustedReaders: trusted}, nil
	}
	configSource := trustConfigSource(s)
	if _, err := migrateLegacyIdentity(s, identity, capabilities); err != nil {
		return untrustedEvaluation(), err
	}
	if s.OfficialCatalogEntryID != "" && mcpcatalog.RuntimeEntryRevoked(s.OfficialCatalogEntryID) {
		return untrustedEvaluation(), nil
	}
	if s.OfficialCatalogEntryID != "" {
		denied, err := manager.OfficialDenied(s.OfficialCatalogEntryID)
		if err != nil {
			return untrustedEvaluation(), err
		}
		if denied {
			return untrustedEvaluation(), nil
		}
	}
	hasReceipt, err := manager.HasReceipt(s.Name, configSource)
	if err != nil {
		return untrustedEvaluation(), err
	}
	if !hasReceipt {
		if s.OfficialCatalogEntryID != "" {
			if err := manager.TrustOfficial(s.Name, configSource, identity, s.OfficialCatalogEntryID, capabilities, s.OfficialReaderNames); err != nil {
				return untrustedEvaluation(), err
			}
			return managerEvaluate(manager, s, identity, capabilities)
		}
		if s.AutoTrust {
			if err := manager.Trust(mcptrust.ScopeWorkspace, mcptrust.SourceUser, s.Name, configSource, identity, "", capabilities); err != nil {
				return untrustedEvaluation(), err
			}
			return managerEvaluate(manager, s, identity, capabilities)
		}
		legacyConfigured := len(s.ReadOnlyToolNames) > 0 || len(s.ReadOnlyModelToolNames) > 0
		legacyImported, err := manager.LegacyImported(s.Name, configSource)
		if err != nil {
			return untrustedEvaluation(), err
		}
		if legacyConfigured && !legacyImported {
			legacy := make([]mcptrust.Capability, 0)
			for _, cap := range capabilities {
				visible := strings.TrimPrefix(cap.ModelName, ToolPrefix(s.Name))
				if s.toolReadOnlyOverride(cap.RawName, visible) && cap.ReadOnly && !cap.Destructive {
					legacy = append(legacy, cap)
				}
			}
			if len(legacy) > 0 {
				if err := manager.Trust(mcptrust.ScopeWorkspace, mcptrust.SourceLegacyImport, s.Name, configSource, identity, "", legacy); err != nil {
					return untrustedEvaluation(), err
				}
			}
			if err := manager.MarkLegacyImported(s.Name, configSource); err != nil {
				return untrustedEvaluation(), err
			}
		}
	}
	eval, err := managerEvaluate(manager, s, identity, capabilities)
	if err != nil {
		return untrustedEvaluation(), err
	}
	if s.AutoTrust && (eval.IdentityChanged || len(eval.ChangedTools) > 0) {
		if err := manager.Trust(mcptrust.ScopeWorkspace, mcptrust.SourceUser, s.Name, configSource, identity, "", capabilities); err != nil {
			return untrustedEvaluation(), err
		}
		return managerEvaluate(manager, s, identity, capabilities)
	}
	return eval, nil
}

func trustConfigSource(s Spec) string {
	if s.OfficialCatalogEntryID != "" {
		return "official_catalog:" + s.OfficialCatalogEntryID
	}
	return s.ConfigSource
}

func capabilityFingerprint(cap mcptrust.Capability) string {
	fp, _ := mcptrust.CapabilityFingerprint(cap)
	return fp
}

// CachedToolTrust is the host-local safety classification for one tool in an
// identity-matched schema cache. It is used by strict read-only sessions to
// decide whether an unconnected MCP reader may start without granting trust or
// invoking the server during resolution.
type CachedToolTrust struct {
	TrustedReader         bool
	Destructive           bool
	CapabilityFingerprint string
	TrustState            mcptrust.TrustState
}

// CachedToolTrustForSpec evaluates an already-established receipt against the
// exact cached server identity and capability snapshot. It never creates or
// upgrades trust: first trust and drift review remain parent-session actions.
func CachedToolTrustForSpec(ctx context.Context, s Spec, rawName string) (CachedToolTrust, bool, error) {
	cs, ok := LoadCachedSchemaForSpec(s)
	if !ok {
		return CachedToolTrust{}, false, nil
	}
	caps := make([]mcptrust.Capability, 0, len(cs.Tools))
	var target *mcptrust.Capability
	for _, cached := range cs.Tools {
		visible := cached.Name
		if s.StripRawPrefix != "" {
			visible = strings.TrimPrefix(visible, s.StripRawPrefix)
		}
		cap := mcptrust.Capability{
			RawName: cached.Name, ModelName: toolName(s.Name, visible),
			InputSchema: cached.Schema, OutputSchema: cached.OutputSchema,
			ReadOnly:    cached.ReadOnly || s.toolReadOnlyOverride(cached.Name, visible),
			Destructive: cached.Destructive,
		}
		caps = append(caps, cap)
		if cached.Name == rawName {
			copy := cap
			target = &copy
		}
	}
	if target == nil {
		return CachedToolTrust{}, false, nil
	}
	result := CachedToolTrust{
		Destructive:           target.Destructive,
		CapabilityFingerprint: capabilityFingerprint(*target),
		TrustState:            mcptrust.TrustUntrusted,
	}
	if s.TrustManager == nil {
		result.TrustedReader = target.ReadOnly && !target.Destructive
		return result, true, nil
	}
	if s.OfficialCatalogEntryID != "" && mcpcatalog.RuntimeEntryRevoked(s.OfficialCatalogEntryID) {
		return result, true, nil
	}
	if s.OfficialCatalogEntryID != "" {
		denied, err := s.TrustManager.OfficialDenied(s.OfficialCatalogEntryID)
		if err != nil {
			return result, true, err
		}
		if denied {
			return result, true, nil
		}
	}
	hasReceipt, err := s.TrustManager.HasReceipt(s.Name, trustConfigSource(s))
	if err != nil {
		return result, true, err
	}
	if !hasReceipt {
		return result, true, nil
	}
	locked, err := applyStoredLauncherLock(s)
	if err != nil {
		return result, true, err
	}
	identity, err := specIdentityFingerprint(ctx, locked)
	if err != nil {
		return result, true, err
	}
	eval, err := managerEvaluate(s.TrustManager, s, identity, caps)
	if err != nil {
		return result, true, err
	}
	result.TrustState = eval.State
	result.TrustedReader = eval.TrustedReaders[target.RawName] && !target.Destructive
	return result, true, nil
}
