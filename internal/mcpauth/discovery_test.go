package mcpauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHttpOrigin(t *testing.T) {
	cases := map[string]string{
		"https://mcp.example.com/path": "https://mcp.example.com",
		"http://localhost:8080/mcp":    "http://localhost:8080",
		"https://host:443/x":           "https://host",
		"http://host:80/x":             "http://host",
		"ftp://host/x":                 "",
		"/relative":                    "",
		"https://host:9000/":           "https://host:9000",
	}
	for in, want := range cases {
		got, ok := httpOrigin(in)
		if want == "" {
			if ok {
				t.Errorf("httpOrigin(%q) = %q, want none", in, got)
			}
			continue
		}
		if got != want {
			t.Errorf("httpOrigin(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProtectedResourceMetadataURL(t *testing.T) {
	got := protectedResourceMetadataURL("https://mcp.example.com/some/path")
	want := "https://mcp.example.com/.well-known/oauth-protected-resource"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if protectedResourceMetadataURL("not a url") != "" {
		t.Fatal("non-http(s) should yield empty")
	}
}

func TestAuthorizationServerMetadataURL(t *testing.T) {
	cases := map[string]string{
		"https://auth.example.com":                                        "https://auth.example.com/.well-known/oauth-authorization-server",
		"https://auth.example.com/":                                       "https://auth.example.com/.well-known/oauth-authorization-server",
		"https://auth.example.com/.well-known/oauth-authorization-server": "https://auth.example.com/.well-known/oauth-authorization-server",
		"": "",
	}
	for in, want := range cases {
		if got := authorizationServerMetadataURL(in); got != want {
			t.Errorf("authorizationServerMetadataURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func newTestClient(t *testing.T, opts Options) *Client {
	t.Helper()
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 0}
	}
	c, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// jsonHandler serves a JSON body and records that it was hit.
func jsonHandler(t *testing.T, body any) http.HandlerFunc {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}
}

func TestFetchProtectedResourceMetadata(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, ProtectedResourceMetadata{
			Resource:             srv.URL,
			AuthorizationServers: []string{"https://auth.example.com"},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, Options{})
	meta, err := c.fetchProtectedResourceMetadata(context.Background(), srv.URL, "", nil)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(meta.AuthorizationServers) != 1 {
		t.Fatalf("want 1 auth server, got %v", meta.AuthorizationServers)
	}
}

func TestFetchProtectedResourceMetadataRejectsForeignResource(t *testing.T) {
	srv := httptest.NewServer(jsonHandler(t, ProtectedResourceMetadata{
		Resource:             "https://evil.example.com",
		AuthorizationServers: []string{"https://auth.example.com"},
	}))
	defer srv.Close()

	c := newTestClient(t, Options{})
	_, err := c.fetchProtectedResourceMetadata(context.Background(), srv.URL, "", nil)
	if err == nil {
		t.Fatal("expected error for resource mismatch")
	}
}

func TestFetchAuthServerMetadataRejectsMissingFields(t *testing.T) {
	srv := httptest.NewServer(jsonHandler(t, AuthorizationServerMetadata{
		Issuer: "https://auth.example.com",
		// missing endpoints
	}))
	defer srv.Close()

	c := newTestClient(t, Options{})
	_, err := c.fetchAuthorizationServerMetadata(context.Background(), srv.URL, srv.URL, nil)
	if err == nil {
		t.Fatal("expected error for missing token_endpoint")
	}
}
