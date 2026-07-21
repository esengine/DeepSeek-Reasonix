package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// fakeAuthorizer is a test MCPAuthorizer: CurrentToken returns nothing until
// Authorize has been called, after which it serves the token.
type fakeAuthorizer struct {
	token      string
	authCalled atomic.Int32
}

func (f *fakeAuthorizer) CurrentToken(_ string) string {
	if f.authCalled.Load() > 0 {
		return f.token
	}
	return ""
}

func (f *fakeAuthorizer) Authorize(context.Context, string, string) (string, error) {
	f.authCalled.Add(1)
	return f.token, nil
}

// TestHTTPTransportOAuthRetry verifies that a 401 triggers the authorizer and
// the request is retried once with the returned bearer token.
func TestHTTPTransportOAuthRetry(t *testing.T) {
	var unauthorizedHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer SECRET" {
			unauthorizedHits.Add(1)
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://example/.well-known/oauth-protected-resource"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Minimal valid initialize response.
		var req struct {
			ID int `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + itoa(req.ID) + `,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"test","version":"1"}}}`))
	}))
	defer srv.Close()

	auth := &fakeAuthorizer{token: "Bearer SECRET"}
	transport, err := newHTTPTransport(Spec{Name: "oauth-srv", Type: "http", URL: srv.URL, Authorizer: auth})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()

	res, err := transport.call(context.Background(), "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "reasonix", "version": "test"},
	})
	if err != nil {
		t.Fatalf("call after OAuth retry: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("expected a non-empty initialize result")
	}
	if auth.authCalled.Load() != 1 {
		t.Fatalf("expected Authorize to be called once, got %d", auth.authCalled.Load())
	}
	if unauthorizedHits.Load() != 1 {
		t.Fatalf("expected exactly one 401 (first attempt), got %d", unauthorizedHits.Load())
	}
}

// TestHTTPTransportOAuthDisabledKeeps401 verifies that without an authorizer a
// 401 surfaces as an error rather than triggering any flow.
func TestHTTPTransportOAuthDisabledKeeps401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	transport, err := newHTTPTransport(Spec{Name: "noauth", Type: "http", URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.close()

	if _, err := transport.call(context.Background(), "initialize", map[string]any{}); err == nil {
		t.Fatal("expected an error for 401 with no authorizer")
	}
}

// itoa is a tiny strconv-free int->string to keep the test dependency-light.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
