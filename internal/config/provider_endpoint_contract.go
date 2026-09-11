package config

import (
	"fmt"
	"net/url"
	"strings"
)

// ProviderEndpointMismatch describes a high-confidence conflict between a
// selected protocol and an exact request URL. It is intentionally conservative:
// custom gateways and query-bearing routes remain user-owned.
type ProviderEndpointMismatch struct {
	Protocol    string
	RequestURL  string
	Recommended string
}

func (e *ProviderEndpointMismatch) Error() string {
	if e == nil {
		return ""
	}
	if e.Recommended != "" {
		return fmt.Sprintf("provider endpoint %q does not match protocol %q; use %s", e.RequestURL, e.Protocol, e.Recommended)
	}
	return fmt.Sprintf("provider endpoint %q does not match protocol %q", e.RequestURL, e.Protocol)
}

func normalizedProviderProtocol(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "dashscope-responses" {
		return "responses"
	}
	return kind
}

func providerProtocolSuffix(kind string) string {
	switch normalizedProviderProtocol(kind) {
	case "anthropic":
		return "/messages"
	case "responses":
		return "/responses"
	case "openai":
		return "/chat/completions"
	default:
		return ""
	}
}

// ProviderRequestURL builds the complete request URL represented by one SDK
// base URL in the protocol registry.
func ProviderRequestURL(kind, baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	switch normalizedProviderProtocol(kind) {
	case "anthropic":
		if strings.HasSuffix(base, "/v1") {
			return base + "/messages"
		}
		return base + "/v1/messages"
	case "responses":
		return base + "/responses"
	case "openai":
		return base + "/chat/completions"
	default:
		return base
	}
}

// ProviderEffectiveRequestURL resolves current and legacy endpoint fields with
// the same precedence used by the runtime adapters.
func ProviderEffectiveRequestURL(e *ProviderEntry) string {
	if e == nil {
		return ""
	}
	if requestURL := strings.TrimSpace(e.RequestURL); requestURL != "" {
		return requestURL
	}
	if normalizedProviderProtocol(e.Kind) == "openai" {
		if chatURL := strings.TrimRight(strings.TrimSpace(e.ChatURL), "/"); chatURL != "" {
			return chatURL
		}
	}
	return ProviderRequestURL(e.Kind, e.BaseURL)
}

// CatalogForProviderEntry resolves metadata for installed connections. Falling
// back to Name keeps hidden legacy presets useful without listing them for new
// connections.
func CatalogForProviderEntry(e *ProviderEntry) (string, ProviderCatalog, bool) {
	if e == nil {
		return "", ProviderCatalog{}, false
	}
	ids := []string{strings.TrimSpace(e.PresetID), strings.TrimSpace(e.Name)}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if preset, ok := CuratedProviderPreset(id); ok {
			return preset.ID, CatalogForProviderPreset(preset), true
		}
	}
	return "", ProviderCatalog{}, false
}

func recommendedProviderRequestURL(kind string, catalog ProviderCatalog) string {
	route, ok := catalog.Protocols[normalizedProviderProtocol(kind)]
	if !ok {
		return ""
	}
	return ProviderRequestURL(kind, route.BaseURL)
}

// ProviderEndpointMismatchForEntry validates only explicit, recognizable
// conflicts. Unknown paths, hosts, query strings and fragments are preserved.
func ProviderEndpointMismatchForEntry(e *ProviderEntry) *ProviderEndpointMismatch {
	if e == nil {
		return nil
	}
	kind := normalizedProviderProtocol(e.Kind)
	expectedSuffix := providerProtocolSuffix(kind)
	requestURL := ProviderEffectiveRequestURL(e)
	if expectedSuffix == "" || requestURL == "" {
		return nil
	}
	u, err := url.Parse(requestURL)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	recommended := ""
	_, catalog, hasCatalog := CatalogForProviderEntry(e)
	if hasCatalog {
		recommended = recommendedProviderRequestURL(kind, catalog)
		if recommendedURL, parseErr := url.Parse(recommended); parseErr == nil &&
			strings.EqualFold(recommendedURL.Scheme, u.Scheme) &&
			strings.EqualFold(recommendedURL.Host, u.Host) &&
			strings.TrimRight(recommendedURL.EscapedPath(), "/") == path {
			return nil
		}
	}
	for _, suffix := range []string{"/v1/messages", "/messages", "/chat/completions", "/responses"} {
		if strings.HasSuffix(path, suffix) && !strings.HasSuffix(path, expectedSuffix) {
			return &ProviderEndpointMismatch{Protocol: kind, RequestURL: requestURL, Recommended: recommended}
		}
	}
	if !hasCatalog || !strings.HasSuffix(path, expectedSuffix) {
		return nil
	}
	selectedRoute, selected := catalog.Protocols[kind]
	if !selected {
		return nil
	}
	selectedBase, err := url.Parse(selectedRoute.BaseURL)
	if err != nil || !strings.EqualFold(selectedBase.Host, u.Host) {
		return nil
	}
	for otherKind, route := range catalog.Protocols {
		if normalizedProviderProtocol(otherKind) == kind {
			continue
		}
		otherBase, parseErr := url.Parse(ProviderRequestURL(otherKind, route.BaseURL))
		if parseErr != nil || !strings.EqualFold(otherBase.Host, u.Host) {
			continue
		}
		otherSuffix := providerProtocolSuffix(otherKind)
		foreignRequestPath := strings.TrimRight(otherBase.EscapedPath(), "/")
		foreignRoot := strings.TrimSuffix(foreignRequestPath, otherSuffix)
		if foreignRoot == "" || foreignRoot == "/" {
			continue
		}
		if path == foreignRoot+expectedSuffix {
			return &ProviderEndpointMismatch{Protocol: kind, RequestURL: requestURL, Recommended: recommended}
		}
	}
	return nil
}

func ValidateProviderEndpoint(e *ProviderEntry) error {
	if mismatch := ProviderEndpointMismatchForEntry(e); mismatch != nil {
		return mismatch
	}
	return nil
}
