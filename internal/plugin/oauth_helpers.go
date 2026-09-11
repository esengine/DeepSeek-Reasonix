package plugin

import (
	"fmt"
	"net/url"
	"strings"
)

func oauthResourceAndIssuer(metadata protectedResourceMetadata, endpoint *url.URL) (string, *url.URL, error) {
	resource := strings.TrimSpace(metadata.Resource)
	if resource == "" {
		resource = endpoint.String()
	}
	if !sameCanonicalResource(resource, endpoint.String()) {
		return "", nil, fmt.Errorf("MCP OAuth: protected resource %q does not match configured endpoint %q", resource, endpoint.String())
	}
	if len(metadata.AuthorizationServers) == 0 {
		return "", nil, fmt.Errorf("MCP OAuth: protected resource metadata has no authorization_servers")
	}
	issuer, err := parseSecureOAuthURL(metadata.AuthorizationServers[0], true)
	if err != nil {
		return "", nil, fmt.Errorf("MCP OAuth authorization server: %w", err)
	}
	return resource, issuer, nil
}

func skipAuthSeparators(raw string, i int) int {
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == ',') {
		i++
	}
	return i
}

func skipAuthWhitespace(raw string, i int) int {
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
		i++
	}
	return i
}

func scanAuthToken(raw string, i int) int {
	for i < len(raw) && (raw[i] == '-' || raw[i] == '_' || raw[i] >= '0' && raw[i] <= '9' || raw[i] >= 'a' && raw[i] <= 'z' || raw[i] >= 'A' && raw[i] <= 'Z') {
		i++
	}
	return i
}

func parseAuthParamValue(raw string, i int) (string, int) {
	var value strings.Builder
	if i < len(raw) && raw[i] == '"' {
		for i++; i < len(raw) && raw[i] != '"'; i++ {
			if raw[i] == '\\' && i+1 < len(raw) {
				i++
			}
			value.WriteByte(raw[i])
		}
		if i < len(raw) {
			i++
		}
		return value.String(), i
	}
	for i < len(raw) && raw[i] != ',' && raw[i] != ' ' && raw[i] != '\t' {
		value.WriteByte(raw[i])
		i++
	}
	return value.String(), i
}
