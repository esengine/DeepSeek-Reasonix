package mcpauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxMetadataBody caps how much of a metadata document we read.
const maxMetadataBody = 1 << 20 // 1 MiB

// httpOrigin returns the scheme://host[:port] of an absolute http(s) URL. The
// port is omitted when it is the scheme default. Non-http(s) or relative URLs
// return ("", false).
func httpOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return "", false
	}
	host := u.Hostname()
	if host == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	port := u.Port()
	if port != "" && !((scheme == "http" && port == "80") || (scheme == "https" && port == "443")) {
		return scheme + "://" + host + ":" + port, true
	}
	return scheme + "://" + host, true
}

// serverOrigin is the canonical store key for serverURL: its http(s) origin.
func serverOrigin(serverURL string) string {
	origin, _ := httpOrigin(serverURL)
	return origin
}

// protectedResourceMetadataURL returns the RFC 9728 well-known URL for the MCP
// server origin, or "" when serverURL is not an absolute http(s) URL.
func protectedResourceMetadataURL(serverURL string) string {
	origin, ok := httpOrigin(serverURL)
	if !ok {
		return ""
	}
	return origin + "/.well-known/oauth-protected-resource"
}

// authorizationServerMetadataURL returns the RFC 8414 metadata URL for an
// authorization-server origin/issuer. If asURL already points at a well-known
// document it is used as-is; otherwise the well-known suffix is appended.
func authorizationServerMetadataURL(asURL string) string {
	asURL = strings.TrimRight(strings.TrimSpace(asURL), "/")
	if asURL == "" {
		return ""
	}
	if strings.Contains(asURL, "/.well-known/oauth-authorization-server") {
		return asURL
	}
	return asURL + "/.well-known/oauth-authorization-server"
}

// fetchProtectedResourceMetadata fetches and validates the RFC 9728 metadata for
// serverURL. headerURL (from a 401 WWW-Authenticate) is preferred when present
// and trusted; otherwise the origin's well-known path is used.
func (c *Client) fetchProtectedResourceMetadata(ctx context.Context, serverURL, headerURL string, trusted []string) (*ProtectedResourceMetadata, error) {
	origin, ok := httpOrigin(serverURL)
	if !ok {
		return nil, fmt.Errorf("server URL %q is not an absolute http(s) URL", serverURL)
	}
	allowed := append([]string{origin}, trusted...)

	candidates := protectedResourceMetadataURL(serverURL)
	if headerURL = strings.TrimSpace(headerURL); headerURL != "" && originAllowed(headerURL, allowed) {
		candidates = headerURL
	}

	var meta ProtectedResourceMetadata
	if err := c.getJSON(ctx, candidates, &meta); err != nil {
		return nil, fmt.Errorf("fetch protected-resource metadata: %w", err)
	}
	// RFC 9728 §2.1: the resource field must match the request origin unless the
	// operator trusts another origin. This stops a server from advertising
	// another resource's metadata.
	if meta.Resource != "" && !originEqual(meta.Resource, origin) && !originAllowed(meta.Resource, allowed) {
		return nil, fmt.Errorf("protected-resource metadata resource %q does not match server origin %q", meta.Resource, origin)
	}
	if len(meta.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("protected-resource metadata lists no authorization servers")
	}
	return &meta, nil
}

// fetchAuthorizationServerMetadata fetches and validates the RFC 8414 metadata
// for authorizationServer (an origin or issuer URL).
func (c *Client) fetchAuthorizationServerMetadata(ctx context.Context, serverOriginValue, authorizationServer string, trusted []string) (*AuthorizationServerMetadata, error) {
	metaURL := authorizationServerMetadataURL(authorizationServer)
	if metaURL == "" {
		return nil, fmt.Errorf("empty authorization server URL")
	}
	allowed := append([]string{serverOriginValue, authorizationServer}, trusted...)

	var meta AuthorizationServerMetadata
	if err := c.getJSON(ctx, metaURL, &meta); err != nil {
		return nil, fmt.Errorf("fetch authorization-server metadata: %w", err)
	}
	if meta.Issuer == "" || meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
		return nil, fmt.Errorf("authorization-server metadata at %s is missing required fields (issuer/authorization_endpoint/token_endpoint)", metaURL)
	}
	// RFC 8414 §3: the issuer in the metadata must match the URL it was
	// retrieved from unless the operator trusts the issuer origin.
	expectedIssuer := strings.TrimSuffix(metaURL, "/.well-known/oauth-authorization-server")
	if !originEqual(meta.Issuer, expectedIssuer) && !originEqual(meta.Issuer, authorizationServer) && !originAllowed(meta.Issuer, allowed) {
		return nil, fmt.Errorf("authorization-server issuer %q does not match metadata URL %q", meta.Issuer, metaURL)
	}
	if meta.RequirePushedAuthorizationRequests {
		return nil, fmt.Errorf("authorization server requires PAR (RFC 9126), which is not yet supported")
	}
	if !meta.supportsAuthorizationCode() {
		return nil, fmt.Errorf("authorization server does not support the authorization_code grant")
	}
	return &meta, nil
}

// getJSON fetches url and decodes into out.
func (c *Client) getJSON(ctx context.Context, urlStr string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxMetadataBody)).Decode(out)
}

// originEqual reports whether two URLs share the same http(s) origin.
func originEqual(a, b string) bool {
	oa, oka := httpOrigin(a)
	ob, okb := httpOrigin(b)
	if !oka || !okb {
		return false
	}
	return strings.EqualFold(oa, ob)
}

// originAllowed reports whether raw's origin is in the allow list.
func originAllowed(raw string, allowed []string) bool {
	origin, ok := httpOrigin(raw)
	if !ok {
		return false
	}
	for _, a := range allowed {
		if ao, ok := httpOrigin(a); ok && strings.EqualFold(ao, origin) {
			return true
		}
	}
	return false
}
