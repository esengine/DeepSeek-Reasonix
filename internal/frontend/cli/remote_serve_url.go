package cli

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

const remoteServeCapabilityTimeout = 2 * time.Second

// remoteServeBrowserURL keeps fragments for current Serve versions while
// preserving reconnects to already-running legacy processes that only accept
// the query-token bootstrap. Probe failures fall back to the compatible path.
func remoteServeBrowserURL(ctx context.Context, bound, token string) string {
	base := "http://" + bound + "/"
	escapedToken := url.QueryEscape(token)
	if remoteServeSupportsFragmentToken(ctx, base) {
		return base + "#token=" + escapedToken
	}
	return base + "?token=" + escapedToken
}

// remoteServeSupportsFragmentToken probes without sending the secret. Current
// Serve versions expose /auth/token and reject GET with 405; legacy token auth
// rejects the unauthenticated request with 401 before routing the endpoint.
func remoteServeSupportsFragmentToken(ctx context.Context, base string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, remoteServeCapabilityTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, base+"auth/token", nil)
	if err != nil {
		return false
	}
	req.Close = true
	client := &http.Client{
		Transport:     &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusMethodNotAllowed
}
