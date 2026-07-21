// Package mcpauth implements the MCP OAuth 2.0 authorization (2025-06-18 spec)
// for remote MCP servers. It discovers protected-resource (RFC 9728) and
// authorization-server (RFC 8414) metadata, runs the authorization-code + PKCE
// flow (RFC 6749 / RFC 7636) with dynamic client registration (RFC 7591) when
// the server requires it, and persists tokens so later sessions reuse them.
//
// The flow is triggered automatically: when a remote server rejects a request
// with 401, the plugin transport asks a Client for a token. With no token
// cached, the Client opens the user's browser to the server's authorization
// endpoint and listens on a loopback redirect for the code. Tokens are stored
// in a JSON file (0600) keyed by server origin.
package mcpauth

import (
	"strings"
	"time"
)

// refreshSkew is how long before expiry a token is considered stale so a refresh
// can run before the server sees an expired token.
const refreshSkew = 60 * time.Second

// Token is an OAuth 2.0 access token plus the optional refresh token used to
// renew it without another interactive authorization.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"` // "Bearer" when empty
	Scope        string    `json:"scope,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"` // absolute; zero = unknown
}

// Expired reports whether the access token should be treated as stale. A token
// whose expiry is unknown (zero) is considered fresh until the server rejects
// it; one within refreshSkew of expiry is treated as expired so a refresh runs
// first.
func (t Token) Expired() bool {
	if t.AccessToken == "" {
		return true
	}
	if t.Expiry.IsZero() {
		return false
	}
	return time.Now().After(t.Expiry.Add(-refreshSkew))
}

// headerValue renders the value for an "Authorization" header. It defaults to
// the Bearer scheme when the server omits token_type.
func (t Token) headerValue() string {
	tt := strings.TrimSpace(t.TokenType)
	if tt == "" {
		tt = "Bearer"
	}
	return tt + " " + t.AccessToken
}

// ProtectedResourceMetadata is the RFC 9728 protected-resource metadata
// document. MCP serves it from the server origin at
// /.well-known/oauth-protected-resource, or advertises its URL in the
// WWW-Authenticate header of a 401 response.
type ProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	// BearerTokenFormatMethods is informational; ignored.
}

// AuthorizationServerMetadata is the RFC 8414 authorization-server metadata.
// Only the fields Reasonix uses are decoded.
type AuthorizationServerMetadata struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	RegistrationEndpoint          string   `json:"registration_endpoint,omitempty"`
	RevocationEndpoint            string   `json:"revocation_endpoint,omitempty"`
	ScopesSupported               []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported        []string `json:"response_types_supported,omitempty"`
	GrantTypesSupported           []string `json:"grant_types_supported,omitempty"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported,omitempty"`
	// RequirePushedAuthorizationRequests, when true, mandates PAR (RFC 9126),
	// which this client does not implement yet.
	RequirePushedAuthorizationRequests bool `json:"require_pushed_authorization_requests,omitempty"`
}

// supportsAuthorizationCode reports whether the AS advertises (or omits, which
// RFC 8414 treats as "all") the authorization_code grant and code response type.
func (m *AuthorizationServerMetadata) supportsAuthorizationCode() bool {
	if m == nil {
		return true
	}
	if len(m.GrantTypesSupported) > 0 && !containsAny(m.GrantTypesSupported, "authorization_code") {
		return false
	}
	if len(m.ResponseTypesSupported) > 0 && !containsAny(m.ResponseTypesSupported, "code") {
		return false
	}
	return true
}

// supportsS256 reports whether the AS advertises PKCE S256 (or omits the list,
// which we treat as permissive).
func (m *AuthorizationServerMetadata) supportsS256() bool {
	if m == nil || len(m.CodeChallengeMethodsSupported) == 0 {
		return true
	}
	for _, method := range m.CodeChallengeMethodsSupported {
		if strings.EqualFold(strings.TrimSpace(method), "S256") {
			return true
		}
	}
	return false
}

func containsAny(haystack []string, needles ...string) bool {
	for _, h := range haystack {
		lh := strings.ToLower(strings.TrimSpace(h))
		for _, n := range needles {
			if lh == strings.ToLower(strings.TrimSpace(n)) {
				return true
			}
		}
	}
	return false
}

// ClientRegistration is the result of RFC 7591 dynamic client registration.
type ClientRegistration struct {
	ClientID              string    `json:"client_id"`
	ClientSecret          string    `json:"client_secret,omitempty"`
	ClientIDIssuedAt      time.Time `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt time.Time `json:"client_secret_expires_at,omitempty"` // zero = never
}

// hasSecret reports whether this is a confidential client.
func (r *ClientRegistration) hasSecret() bool {
	return r != nil && strings.TrimSpace(r.ClientSecret) != ""
}

// ServerMetadata captures the discovered endpoints for one server so a later
// refresh or re-authorization does not have to re-run discovery.
type ServerMetadata struct {
	ResourceURL       string                       `json:"resource_url"` // MCP server origin
	ProtectedResource *ProtectedResourceMetadata   `json:"protected_resource,omitempty"`
	AuthServer        *AuthorizationServerMetadata `json:"auth_server,omitempty"`
}

// StoredCredential bundles everything the client must remember for one server
// origin to refresh or re-authorize without re-discovering metadata.
type StoredCredential struct {
	Token    Token               `json:"token"`
	Server   *ServerMetadata     `json:"server,omitempty"`
	Client   *ClientRegistration `json:"client,omitempty"`
	IssuedAt time.Time           `json:"issued_at"`
}

// Config is the optional, user-facing OAuth configuration for one MCP server.
// With no config the client auto-discovers via the 401 flow.
type Config struct {
	// ClientID overrides the public client id. When empty, the client performs
	// dynamic client registration if the AS advertises a registration_endpoint;
	// otherwise it uses a generated public client id.
	ClientID string `json:"client_id,omitempty" toml:"client_id,omitempty"`
	// ClientSecret pairs with ClientID for confidential clients. Leave empty
	// for public PKCE clients.
	ClientSecret string `json:"client_secret,omitempty" toml:"client_secret,omitempty"`
	// Scopes requests specific OAuth scopes. Empty lets the AS decide.
	Scopes []string `json:"scopes,omitempty" toml:"scopes,omitempty"`
	// RedirectPort pins the loopback callback port. Zero picks a free port.
	RedirectPort int `json:"redirect_port,omitempty" toml:"redirect_port,omitempty"`
	// SkipBrowser prints the authorization URL instead of opening a browser
	// (headless or CI).
	SkipBrowser bool `json:"skip_browser,omitempty" toml:"skip_browser,omitempty"`
	// SkipDynamicRegistration disables RFC 7591 registration even if the AS
	// advertises a registration_endpoint.
	SkipDynamicRegistration bool `json:"skip_dynamic_registration,omitempty" toml:"skip_dynamic_registration,omitempty"`
	// TrustedOrigins authorizes metadata/issuer origins outside the MCP server
	// origin. By default metadata must come from the server origin or its
	// advertised auth server; this is an escape hatch for federated deployments.
	TrustedOrigins []string `json:"trusted_origins,omitempty" toml:"trusted_origins,omitempty"`
}
