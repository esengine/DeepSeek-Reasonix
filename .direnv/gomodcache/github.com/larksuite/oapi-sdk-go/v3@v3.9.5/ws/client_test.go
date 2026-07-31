package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

type wsMockClientAssertionProvider struct {
	mu     sync.Mutex
	tokens []*larkcore.Token
	index  int
	calls  int
	auds   []string
}

func (p *wsMockClientAssertionProvider) RetrieveToken(ctx context.Context, aud string) (*larkcore.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.auds = append(p.auds, aud)
	token := p.tokens[p.index]
	if p.index < len(p.tokens)-1 {
		p.index++
	}
	return token, nil
}

func TestGetConnURLWithAppSecret(t *testing.T) {
	originalClient := bootstrapHTTPClient
	defer func() { bootstrapHTTPClient = originalClient }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != GenEndpointUri {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req BootstrapRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		if req.AppSecret == "" || req.ClientAssertion != "" {
			t.Fatalf("unexpected bootstrap request: %#v", req)
		}
		_ = json.NewEncoder(w).Encode(&EndpointResp{Code: OK, Data: &Endpoint{Url: "wss://example.com/ws"}})
	}))
	defer server.Close()

	bootstrapHTTPClient = server.Client()
	client := NewClient("app-id", "app-secret", WithDomain(server.URL))
	connURL, err := client.getConnURL(context.Background())
	if err != nil {
		t.Fatalf("get conn url failed: %v", err)
	}
	if connURL != "wss://example.com/ws" {
		t.Fatalf("unexpected conn url: %s", connURL)
	}
}

func TestGetConnURLWithCustomHeadersAndUserAgent(t *testing.T) {
	originalClient := bootstrapHTTPClient
	defer func() { bootstrapHTTPClient = originalClient }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Tt-Env") != "ppe_test" {
			t.Fatalf("missing X-Tt-Env header: %s", r.Header.Get("X-Tt-Env"))
		}
		if r.Header.Get("X-Use-Ppe") != "1" {
			t.Fatalf("missing X-Use-Ppe header: %s", r.Header.Get("X-Use-Ppe"))
		}
		userAgent := r.Header.Get("User-Agent")
		if !strings.HasPrefix(userAgent, "oapi-sdk-go/") {
			t.Fatalf("unexpected user agent: %s", userAgent)
		}
		if !strings.Contains(userAgent, " source/ws-test") {
			t.Fatalf("missing source in user agent: %s", userAgent)
		}
		_ = json.NewEncoder(w).Encode(&EndpointResp{Code: OK, Data: &Endpoint{Url: "wss://example.com/ws"}})
	}))
	defer server.Close()

	headers := make(http.Header)
	headers.Set("X-Tt-Env", "ppe_test")
	headers.Set("X-Use-Ppe", "1")

	bootstrapHTTPClient = server.Client()
	client := NewClient("app-id", "app-secret",
		WithDomain(server.URL),
		WithHeaders(headers),
		WithSource("ws-test"),
	)
	if _, err := client.getConnURL(context.Background()); err != nil {
		t.Fatalf("get conn url failed: %v", err)
	}
}

func TestGetConnURLWithClientAssertionProxy(t *testing.T) {
	originalClient := bootstrapHTTPClient
	defer func() { bootstrapHTTPClient = originalClient }()

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy"+GenEndpointUri {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get(larkcore.HeaderXTargetService) != "open.feishu.cn" {
			t.Fatalf("unexpected target service: %s", r.Header.Get(larkcore.HeaderXTargetService))
		}
		var req BootstrapRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		if req.ClientAssertion != "assertion" || req.AppSecret != "" {
			t.Fatalf("unexpected bootstrap request: %#v", req)
		}
		_ = json.NewEncoder(w).Encode(&EndpointResp{Code: OK, Data: &Endpoint{Url: "wss://example.com/ws"}})
	}))
	defer proxyServer.Close()

	bootstrapHTTPClient = proxyServer.Client()
	provider := &wsMockClientAssertionProvider{tokens: []*larkcore.Token{{Value: "assertion", TargetInfo: &larkcore.TargetInfo{TargetService: proxyServer.URL, TargetPrefix: "/proxy"}}}}
	client := NewClient("app-id", "", WithDomain("https://open.feishu.cn"), WithClientAssertionProvider(provider))
	if _, err := client.getConnURL(context.Background()); err != nil {
		t.Fatalf("get conn url failed: %v", err)
	}
}

func TestGetConnURLWithClientAssertionProxyHTTPErrorMsg(t *testing.T) {
	originalClient := bootstrapHTTPClient
	defer func() { bootstrapHTTPClient = originalClient }()

	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}{
			Code: 20050,
			Msg:  "target service unavailable",
		})
	}))
	defer proxyServer.Close()

	bootstrapHTTPClient = proxyServer.Client()
	provider := &wsMockClientAssertionProvider{tokens: []*larkcore.Token{{Value: "assertion", TargetInfo: &larkcore.TargetInfo{TargetService: proxyServer.URL, TargetPrefix: "/proxy"}}}}
	client := NewClient("app-id", "", WithDomain("https://open.feishu.cn"), WithClientAssertionProvider(provider))
	_, err := client.getConnURL(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	serverErr, ok := err.(*ServerError)
	if !ok {
		t.Fatalf("unexpected error type: %#v", err)
	}
	if serverErr.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected server error code: %d", serverErr.Code)
	}
	if serverErr.Msg != "target service unavailable" {
		t.Fatalf("unexpected server error msg: %s", serverErr.Msg)
	}
}

func TestGetConnURLRetrieveTokenEachTime(t *testing.T) {
	originalClient := bootstrapHTTPClient
	defer func() { bootstrapHTTPClient = originalClient }()

	bodyAssertions := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req BootstrapRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}
		bodyAssertions = append(bodyAssertions, req.ClientAssertion)
		_ = json.NewEncoder(w).Encode(&EndpointResp{Code: OK, Data: &Endpoint{Url: "wss://example.com/ws"}})
	}))
	defer server.Close()

	bootstrapHTTPClient = server.Client()
	provider := &wsMockClientAssertionProvider{tokens: []*larkcore.Token{{Value: "assertion-1"}, {Value: "assertion-2"}}}
	client := NewClient("app-id", "", WithDomain(server.URL), WithClientAssertionProvider(provider))
	if _, err := client.getConnURL(context.Background()); err != nil {
		t.Fatalf("first get conn url failed: %v", err)
	}
	if _, err := client.getConnURL(context.Background()); err != nil {
		t.Fatalf("second get conn url failed: %v", err)
	}
	if provider.calls != 2 {
		t.Fatalf("expected provider called twice, got %d", provider.calls)
	}
	if len(bodyAssertions) != 2 || bodyAssertions[0] != "assertion-1" || bodyAssertions[1] != "assertion-2" {
		t.Fatalf("unexpected assertions: %#v", bodyAssertions)
	}
}

func TestBuildWSProxyURL(t *testing.T) {
	testCases := map[string]string{
		"proxy.example.com":         "https://proxy.example.com/v1" + GenEndpointUri,
		"https://proxy.example.com": "https://proxy.example.com/v1" + GenEndpointUri,
		"http://proxy.example.com":  "http://proxy.example.com/v1" + GenEndpointUri,
	}

	for targetService, expected := range testCases {
		proxyURL := buildWSProxyURL(targetService, "/v1", GenEndpointUri)
		if proxyURL != expected {
			t.Fatalf("unexpected proxy url for %s: %s", targetService, proxyURL)
		}
	}
}
