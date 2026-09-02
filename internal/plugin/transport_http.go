package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpSubscriptionsListenMethod = "subscriptions/listen"

// asyncStreamableHTTPSubscriptions keeps the optional SEP-2575 notification
// stream from becoming part of the mandatory startup critical path. Some MCP
// HTTP bridges buffer a streaming Web Response before writing HTTP response
// headers, so the SDK's synchronous transport write would otherwise block
// Client.Connect even though server/discover already completed successfully.
//
// The underlying call still uses the SDK-owned connection and context. A
// compliant server therefore keeps delivering notifications normally, while a
// buffering server is cancelled with the session without blocking tools/list.
func asyncStreamableHTTPSubscriptions(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
	return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
		if method != mcpSubscriptionsListenMethod {
			return next(ctx, method, req)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		go func() {
			_, _ = next(ctx, method, req)
		}()
		return &mcpsdk.SubscriptionsListenResult{}, nil
	}
}

func newHTTPTransport(s Spec) (*sdkSessionTransport, error) {
	if strings.TrimSpace(s.Type) == "" {
		s.Type = "http"
	}
	// Transient OAuth/probe connections declare no optional capabilities.
	return newSDKSessionTransport(context.Background(), s, HostProfileCore)
}

func validateMCPURL(name, transport, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%s plugin %q: url is required", transport, name)
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s plugin %q: invalid url", transport, name)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("%s plugin %q: url must use http or https", transport, name)
	}
}

func newMCPHTTPClient(lifetime context.Context, s Spec) (*http.Client, error) {
	origin, err := url.Parse(strings.TrimSpace(s.URL))
	if err != nil || origin == nil || origin.Host == "" {
		return nil, fmt.Errorf("invalid MCP endpoint")
	}
	headers := make(map[string]string, len(s.Headers))
	maps.Copy(headers, s.Headers)
	base := http.DefaultTransport.(*http.Transport).Clone()
	client := &http.Client{
		Transport: &sameOriginMCPRoundTripper{
			origin:   origin,
			headers:  headers,
			base:     base,
			lifetime: lifetime,
		},
	}
	client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if sameHTTPOrigin(origin, req.URL) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	return client, nil
}

type sameOriginMCPRoundTripper struct {
	origin   *url.URL
	headers  map[string]string
	base     http.RoundTripper
	lifetime context.Context
}

func (rt *sameOriginMCPRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || !sameHTTPOrigin(rt.origin, req.URL) {
		return nil, errors.New("MCP request changed origin; configured headers were not sent")
	}
	requestCtx := req.Context()
	cancelRequest := func() {}
	stopLifetime := func() bool { return true }
	// Keep protocol cleanup independent from the session lifetime: Close first
	// cancels active GET/POST requests, then the SDK sends this bounded DELETE.
	if req.Method != http.MethodDelete {
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithCancel(req.Context())
		cancelRequest = cancel
		if rt.lifetime != nil {
			stopLifetime = context.AfterFunc(rt.lifetime, cancelRequest)
		}
	}
	cancelLifetimeRequest := func() {
		stopLifetime()
		cancelRequest()
	}
	request := req.Clone(requestCtx)
	request.Header = req.Header.Clone()
	for key, value := range rt.headers {
		request.Header.Set(key, value)
	}

	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	if request.Method != http.MethodDelete {
		response, err := base.RoundTrip(request)
		return responseWithCancel(response, err, cancelLifetimeRequest)
	}

	deleteCtx, cancelDelete := context.WithTimeout(request.Context(), 2*time.Second)
	request = request.Clone(deleteCtx)
	response, err := base.RoundTrip(request)
	return responseWithCancel(response, err, func() {
		cancelDelete()
		cancelLifetimeRequest()
	})
}

func responseWithCancel(response *http.Response, err error, cancel func()) (*http.Response, error) {
	if err != nil {
		cancel()
		return nil, err
	}
	if response.Body == nil {
		cancel()
		return response, nil
	}
	response.Body = &cancelOnCloseBody{ReadCloser: response.Body, cancel: cancel}
	return response, nil
}

func (rt *sameOriginMCPRoundTripper) CloseIdleConnections() {
	if closer, ok := rt.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel func()
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

func sameHTTPOrigin(a, b *url.URL) bool {
	if a == nil || b == nil || !strings.EqualFold(a.Scheme, b.Scheme) || !strings.EqualFold(a.Hostname(), b.Hostname()) {
		return false
	}
	effectivePort := func(u *url.URL) string {
		if port := u.Port(); port != "" {
			return port
		}
		switch strings.ToLower(u.Scheme) {
		case "http":
			return "80"
		case "https":
			return "443"
		default:
			return ""
		}
	}
	return effectivePort(a) == effectivePort(b)
}

func (t *sdkSessionTransport) newEndpoint(ctx context.Context) (sdkEndpoint, error) {
	if t.endpointFactory != nil {
		return t.endpointFactory(ctx)
	}
	switch canonicalMCPRuntimeTransport(t.spec.Type) {
	case "stdio":
		process, err := newStdioTransport(ctx, t.spec)
		if err != nil {
			return sdkEndpoint{}, err
		}
		return sdkEndpoint{
			transport:     &mcpsdk.IOTransport{Reader: process.stdout, Writer: process.stdin},
			close:         process.close,
			startupStderr: process.startupStderr,
		}, nil
	case "streamable-http":
		client, err := newMCPHTTPClient(ctx, t.spec)
		if err != nil {
			return sdkEndpoint{}, err
		}
		return sdkEndpoint{
			transport: &mcpsdk.StreamableClientTransport{
				Endpoint:     t.spec.URL,
				HTTPClient:   client,
				MaxRetries:   5,
				OAuthHandler: t.oauth,
			},
			close: client.CloseIdleConnections,
		}, nil
	case "sse":
		client, err := newMCPHTTPClient(ctx, t.spec)
		if err != nil {
			return sdkEndpoint{}, err
		}
		return sdkEndpoint{
			transport: &mcpsdk.SSEClientTransport{Endpoint: t.spec.URL, HTTPClient: client},
			close:     client.CloseIdleConnections,
		}, nil
	default:
		return sdkEndpoint{}, fmt.Errorf("unknown MCP transport %q", t.spec.Type)
	}
}

// do is retained as a narrow HTTP security test hook. MCP protocol traffic goes
// through the SDK transport above.
func (t *sdkSessionTransport) do(ctx context.Context, body []byte) (*http.Response, error) {
	client, err := newMCPHTTPClient(ctx, t.spec)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.spec.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	return client.Do(req)
}
