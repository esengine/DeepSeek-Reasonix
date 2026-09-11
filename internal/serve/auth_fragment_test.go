package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestTokenModeAllowsOnlyBootstrapShellWithoutAuth(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "secret"})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	for _, path := range []string{"/", "/assets/logo-wordmark.svg", "/sessions/session-123"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, resp.StatusCode)
		}
	}
	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /status = %d, want 401", resp.StatusCode)
	}
}

func TestTokenModeDoesNotPublishNestedSessionLikePaths(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "secret"})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	for _, path := range []string{"/sessions/", "/sessions/a/status", "/sessions"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s status = %d, want 401", path, resp.StatusCode)
		}
	}
}

func TestTokenModeFragmentBootstrapSetsHTTPOnlyCookie(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "secret"})
	passed := false
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		passed = true
		w.WriteHeader(http.StatusOK)
	})))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/auth/token", "application/json", strings.NewReader(`{"token":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("bootstrap status = %d, want 204", resp.StatusCode)
	}
	if passed {
		t.Fatal("bootstrap request must not reach the application handler")
	}
	cookie := findCookie(resp.Cookies(), cookieToken)
	if cookie == nil || !cookie.HttpOnly {
		t.Fatal("bootstrap response must set an HttpOnly token cookie")
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/status", nil)
	req.AddCookie(cookie)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !passed {
		t.Fatalf("cookie-authenticated request status = %d, passed = %v", resp.StatusCode, passed)
	}
}

func TestTokenModeFragmentBootstrapRejectsInvalidRequests(t *testing.T) {
	ag := newAuthGate(config.ServeConfig{AuthMode: "token", Token: "secret"})
	ts := httptest.NewServer(ag.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid bootstrap request reached application handler")
	})))
	defer ts.Close()

	tests := []struct {
		name        string
		contentType string
		body        string
		want        int
	}{
		{name: "wrong token", contentType: "application/json", body: `{"token":"wrong"}`, want: http.StatusUnauthorized},
		{name: "non JSON", contentType: "text/plain", body: `{"token":"secret"}`, want: http.StatusUnsupportedMediaType},
		{name: "malformed JSON", contentType: "application/json", body: `{`, want: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Post(ts.URL+"/auth/token", tt.contentType, strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.want)
			}
		})
	}
}
