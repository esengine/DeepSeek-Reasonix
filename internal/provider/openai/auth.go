package openai

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// newADCTokenSource is swapped in tests. Lazy: no credentials are fetched
// until the first Token() call, so construction stays side-effect free.
var newADCTokenSource = func() (oauth2.TokenSource, error) {
	return google.DefaultTokenSource(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
}

// resolveADCTokenSource returns a token source when extra selects the "adc"
// auth mode, nil otherwise.
func resolveADCTokenSource(extra map[string]any) (oauth2.TokenSource, error) {
	authMode, _ := extra["auth"].(string)
	if authMode != "adc" {
		return nil, nil
	}
	ts, err := newADCTokenSource()
	if err != nil {
		return nil, fmt.Errorf("openai: adc: %w", err)
	}
	return ts, nil
}

// bearerToken returns the Authorization credential for a request: the static
// api_key, or a freshly minted ADC token. ADC tokens live minutes-to-an-hour;
// the source caches internally, so steady state does not re-hit the metadata
// server.
func (c *client) bearerToken() (string, error) {
	if c.adcTokenSource == nil {
		return c.apiKey, nil
	}
	tok, err := c.adcTokenSource.Token()
	if err != nil {
		return "", fmt.Errorf("%s: adc: fetch access token: %w", c.name, err)
	}
	return tok.AccessToken, nil
}

func applyAPIKeyHeader(h http.Header, baseURL, apiKey string) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return
	}
	if IsMiMo(baseURL) {
		h.Set("api-key", apiKey)
		return
	}
	h.Set("Authorization", "Bearer "+apiKey)
}

func applyCustomHeaders(h http.Header, headers map[string]string) {
	for name, value := range cleanCustomHeaders(headers) {
		h.Set(name, value)
	}
}
