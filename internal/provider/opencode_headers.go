package provider

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
)

type cacheSessionContextKey struct{}

// WithCacheSession scopes transport cache routing to a conversation. The hash
// prevents local session paths or user-provided identifiers entering headers.
// It deliberately replaces inherited identities when a child Agent starts.
func WithCacheSession(ctx context.Context, identity string) context.Context {
	sum := sha256.Sum256([]byte(identity))
	return context.WithValue(ctx, cacheSessionContextKey{}, "reasonix-"+hex.EncodeToString(sum[:16]))
}

// NewCacheSessionID provides a stable per-client fallback for standalone calls.
func NewCacheSessionID() string { return "reasonix-" + rand.Text() }

// NewClientIdentityHeaders groups immutable transport identity by client lifetime.
func NewClientIdentityHeaders() http.Header {
	return http.Header{"User-Agent": []string{"Reasonix"}, "X-Opencode-Session": []string{NewCacheSessionID()}}
}

// ApplyOpenCodeGoHeaders implements Go's client identification/cache contract
// for all three wire adapters. It never changes model input or other vendors.
func ApplyOpenCodeGoHeaders(req *http.Request, baseURL string, fallback http.Header) {
	path, ok := officialOpenCodeGoPath(baseURL)
	if !ok || (path != openCodeGoChatPath && path != openCodeGoAnthropicPath) {
		return
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", fallback.Get("User-Agent"))
	}
	if req.Header.Get("x-opencode-session") != "" {
		return
	}
	id, _ := req.Context().Value(cacheSessionContextKey{}).(string)
	if id == "" {
		id = fallback.Get("x-opencode-session")
	}
	if id != "" {
		req.Header.Set("x-opencode-session", id)
	}
}
