package provider_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reasonix/internal/provider"
	"reasonix/internal/provider/anthropic"
	"reasonix/internal/provider/openai"
	"reasonix/internal/provider/responses"
	"testing"
)

func TestOpenCodeGoHeadersAllAdapters(t *testing.T) {
	for _, kind := range []string{"chat", "anthropic", "responses"} {
		t.Run(kind, func(t *testing.T) {
			var headers []http.Header
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				headers = append(headers, r.Header.Clone())
				http.Error(w, "fixture authentication rejection", http.StatusUnauthorized)
			}))
			defer server.Close()
			base := "https://opencode.ai/zen/go/v1"
			if kind == "anthropic" {
				base = "https://opencode.ai/zen/go"
			}
			extra := map[string]any{"request_url": server.URL, "reject_redirects": true}
			var p provider.Provider
			var err error
			cfg := provider.Config{Name: "header-test", BaseURL: base, Model: "deepseek-v4-flash", APIKey: "fixture", Extra: extra}
			switch kind {
			case "chat":
				p, err = openai.New(cfg)
			case "anthropic":
				p, err = anthropic.New(cfg)
			case "responses":
				p = responses.New(responses.Config{Name: cfg.Name, BaseURL: base, Model: cfg.Model, APIKey: cfg.APIKey, RequestURL: server.URL, Mode: "stateless"})
			}
			if err != nil {
				t.Fatal(err)
			}
			defer p.(interface{ CloseIdleConnections() }).CloseIdleConnections()
			for i := range 4 {
				ctx := context.Background()
				if i >= 2 {
					ctx = provider.WithCacheSession(ctx, "resumed-conversation")
				}
				_, err = p.Stream(ctx, provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "fixture"}}})
				if err == nil {
					t.Fatal("expected fixture authentication failure")
				}
			}
			if len(headers) != 4 {
				t.Fatalf("requests=%d", len(headers))
			}
			for _, h := range headers {
				if h.Get("User-Agent") != "Reasonix" || h.Get("x-opencode-session") == "" {
					t.Fatalf("missing identity headers: %v", h)
				}
			}
			id := func(i int) string { return headers[i].Get("x-opencode-session") }
			if id(0) != id(1) || id(2) != id(3) || id(0) == id(2) {
				t.Fatal("client/session header lifetime is incorrect")
			}
		})
	}
}
