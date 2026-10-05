package plugin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// RFC 9207 closes the mix-up attack: a code minted by another authorization
// server arrives with that server's iss, or without the iss this one promised.
func TestOAuthCallbackChecksTheIssuer(t *testing.T) {
	const issuer = "https://auth.example.test"
	cases := []struct {
		name  string
		query string
		req   bool
		ok    bool
	}{
		{"matching iss", "&iss=https://auth.example.test", true, true},
		{"matching iss with a trailing slash", "&iss=https://auth.example.test/", false, true},
		{"another server's iss", "&iss=https://evil.example.test", false, false},
		{"missing iss the server promised", "", true, false},
		{"no iss from a server that never sends one", "", false, true},
	}
	for _, c := range cases {
		result := make(chan oauthCallbackResult, 1)
		h := oauthCallbackHandler(oauthCallbackExpect{state: "s", issuer: issuer, issRequired: c.req}, result)
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s&code=abc"+c.query, nil))
		got := <-result
		if c.ok && (got.Err != nil || got.Code != "abc") {
			t.Fatalf("%s: rejected: %v", c.name, got.Err)
		}
		if !c.ok && !errors.Is(got.Err, ErrOAuthIssuerMismatch) {
			t.Fatalf("%s: err = %v, want ErrOAuthIssuerMismatch", c.name, got.Err)
		}
	}
}
