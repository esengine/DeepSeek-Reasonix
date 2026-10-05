package openai

import (
	"net/http"

	"reasonix/internal/contract/provider"
)

// setChatHeaders stamps the headers every streamed chat POST carries.
func (c *client) setChatHeaders(req *http.Request) {
	if IsDeepSeek(req.URL.String()) {
		// DeepSeek's edge acknowledges an eagerly sent body with WINDOW_UPDATE before
		// its own SETTINGS, which Go's HTTP/2 client rejects (RFC 9113 §3.4). Holding
		// the body for the 100 response lets the server preface arrive first.
		req.Header.Set("Expect", "100-continue")
	}
	req.Header.Set("Content-Type", "application/json")
	applyAPIKeyHeader(req.Header, c.baseURL, c.apiKey())
	req.Header.Set("Accept", "text/event-stream")
	applyCustomHeaders(req.Header, c.headers)
	provider.ApplyOpenCodeGoIdentity(req, c.openCodeSession)
	provider.ApplyClientIdentity(req)
}
