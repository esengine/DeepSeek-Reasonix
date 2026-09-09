package netclient

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ModelProxyOriginalURLHeader is private to the authenticated loopback hop.
// The desktop validates it against the route's frozen provider origins and
// strips it before sending the model request upstream.
const ModelProxyOriginalURLHeader = "X-Reasonix-Model-Proxy-URL"

type modelCredentialProxyTransport struct {
	base     *http.Transport
	endpoint *url.URL
	token    string
}

func NewModelCredentialProxyClient(endpoint, token string) (*http.Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("invalid model credential proxy endpoint")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("model credential proxy requires a loopback endpoint and a token")
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	return &http.Client{Transport: &modelCredentialProxyTransport{base, u, token}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

func (t *modelCredentialProxyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	proxied := request.Clone(request.Context())
	original := request.URL.String()
	proxied.URL.Scheme, proxied.URL.Host = t.endpoint.Scheme, t.endpoint.Host
	proxied.Host = ""
	proxied.Header.Set(ModelProxyOriginalURLHeader, original)
	proxied.Header.Del("x-api-key")
	proxied.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(proxied)
}

func (t *modelCredentialProxyTransport) CloseIdleConnections() { t.base.CloseIdleConnections() }
