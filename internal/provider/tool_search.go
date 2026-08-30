package provider

import (
	"net/url"
	"strings"
	"sync/atomic"
)

// ToolSearch is a request-level native tool-search experiment. Unsupported
// adapters must not serialize it.
type ToolSearch struct {
	Enabled bool
}

// nativeToolSearchPreview is compiled off for 1.33.0. First-party OpenAI
// Responses and Anthropic may enable it later after the preview experiment.
var nativeToolSearchPreview atomic.Bool

func NativeToolSearchPreviewEnabled() bool { return nativeToolSearchPreview.Load() }

func SetNativeToolSearchPreviewForTest(enabled bool) func() {
	prev := nativeToolSearchPreview.Swap(enabled)
	return func() { nativeToolSearchPreview.Store(prev) }
}

type nativeToolSearchProvider interface {
	NativeToolSearchAvailable() bool
}

// NativeToolSearchSupported reports explicit adapter capability. The first
// preview supports first-party OpenAI Responses only; Anthropic stays on the
// fixed proxy until its server-side tool-search result blocks can be replayed.
func NativeToolSearchSupported(p Provider) bool {
	if p == nil {
		return false
	}
	capable, ok := p.(nativeToolSearchProvider)
	return ok && capable.NativeToolSearchAvailable()
}

func NativeToolSearchEnabled(p Provider) bool {
	return nativeToolSearchPreview.Load() && NativeToolSearchSupported(p)
}

// ApplyNativeToolSearch marks extra MCP schemas deferred when the preview is
// active. When disabled it returns visible unchanged so the cache prefix is
// byte-identical to 1.33.0.
func ApplyNativeToolSearch(visible, extra []ToolSchema, p Provider) []ToolSchema {
	if !NativeToolSearchEnabled(p) || len(extra) == 0 {
		return visible
	}
	out := append([]ToolSchema(nil), visible...)
	seen := map[string]bool{}
	for _, schema := range visible {
		seen[schema.Name] = true
	}
	for _, schema := range extra {
		if seen[schema.Name] {
			continue
		}
		schema.Deferred = true
		out = append(out, schema)
		seen[schema.Name] = true
	}
	return out
}

func IsFirstPartyOpenAI(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "api.openai.com" || strings.HasSuffix(host, ".openai.com")
}

func IsFirstPartyAnthropic(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "api.anthropic.com" || strings.HasSuffix(host, ".anthropic.com")
}
