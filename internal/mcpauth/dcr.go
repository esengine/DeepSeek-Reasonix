package mcpauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const clientName = "Reasonix MCP"

// maxTokenBody caps how much of a token-endpoint response we read.
const maxTokenBody = 1 << 20 // 1 MiB

// registerClient performs RFC 7591 dynamic client registration against an
// authorization server, returning a public (PKCE) client registration. The
// redirectURI is the loopback callback URL the authorization flow will use.
func (c *Client) registerClient(ctx context.Context, registrationEndpoint, redirectURI string) (*ClientRegistration, error) {
	if registrationEndpoint == "" {
		return nil, fmt.Errorf("no registration endpoint")
	}
	form := url.Values{
		"client_name":                {clientName},
		"redirect_uris":              {redirectURI},
		"grant_types":                {"authorization_code"},
		"response_types":             {"code"},
		"token_endpoint_auth_method": {"none"}, // public client; PKCE only
	}
	respBody, err := c.postForm(ctx, registrationEndpoint, form, nil)
	if err != nil {
		return nil, fmt.Errorf("dynamic client registration: %w", err)
	}

	// RFC 7591 §3.2: the response is a JSON client metadata document.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("decode registration response: %w", err)
	}
	reg := &ClientRegistration{}
	if v, ok := raw["client_id"]; ok {
		_ = json.Unmarshal(v, &reg.ClientID)
	}
	if strings.TrimSpace(reg.ClientID) == "" {
		return nil, fmt.Errorf("registration response missing client_id")
	}
	if v, ok := raw["client_secret"]; ok {
		_ = json.Unmarshal(v, &reg.ClientSecret)
	}
	reg.ClientIDIssuedAt = time.Now()
	// We intentionally do not enforce client_secret_expires_at: a zero value
	// means "never expires" per RFC 7591 §3.2.1, and an expired secret simply
	// triggers re-registration on the next flow.
	return reg, nil
}

// postForm posts an urlencoded form and returns the raw JSON-decoded response
// object. auth decides how the client authenticates (body credentials for public
// clients, HTTP Basic for confidential ones).
func (c *Client) postForm(ctx context.Context, endpoint string, form url.Values, auth clientAuth) ([]byte, error) {
	if auth != nil {
		auth.enrich(form)
	}
	body := form.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if auth != nil {
		auth.apply(req)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxTokenBody))
	if resp.StatusCode/100 != 2 {
		return respBody, &tokenEndpointError{Status: resp.StatusCode, Body: strings.TrimSpace(string(respBody))}
	}
	return respBody, nil
}

// clientAuth abstracts how the token endpoint authenticates the client.
type clientAuth interface {
	// enrich adds client credentials to the form body (public clients).
	enrich(form url.Values)
	// apply sets request authentication (confidential clients use HTTP Basic).
	apply(req *http.Request)
}

// noneAuth authenticates a public client by putting client_id in the body.
type noneAuth struct{ clientID string }

func (a noneAuth) enrich(form url.Values) { form.Set("client_id", a.clientID) }
func (a noneAuth) apply(*http.Request)    {}

// basicAuth authenticates a confidential client with HTTP Basic (RFC 6749
// §2.3.1). We still send client_id in the body as a fallback for servers that
// reject Basic on public-style flows.
type basicAuth struct{ clientID, clientSecret string }

func (a basicAuth) enrich(form url.Values) { form.Set("client_id", a.clientID) }
func (a basicAuth) apply(req *http.Request) {
	req.SetBasicAuth(url.QueryEscape(a.clientID), url.QueryEscape(a.clientSecret))
}

// tokenEndpointError carries a non-2xx token-endpoint response.
type tokenEndpointError struct {
	Status int
	Body   string
}

func (e *tokenEndpointError) Error() string {
	return fmt.Sprintf("token endpoint http %d: %s", e.Status, e.Body)
}
