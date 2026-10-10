package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Every request that carries a provider key to a configured address must stay
// on that address: a redirect elsewhere is refused before the key is resent.
func TestProviderProbesRefuseACrossOriginRedirect(t *testing.T) {
	for _, tc := range []struct {
		name, path, kind string
		status           int
	}{
		{"single-model check", "/providers/check/model", "openai", http.StatusTemporaryRedirect},
		{"decision probe", "/providers/check/model", "typesafe", http.StatusPermanentRedirect},
		{"endpoint probe", "/providers/probe", "openai", http.StatusFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reached atomic.Int32
			elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				reached.Add(1)
			}))
			defer elsewhere.Close()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, elsewhere.URL+r.URL.Path, tc.status)
			}))
			defer upstream.Close()

			s := newProviderEditServer(t)
			s.AllowProviderEdit()
			srv := httptest.NewServer(operatorHandler(s))
			defer srv.Close()
			body := fmt.Sprintf(`{"name":"not-saved","model":"m","baseUrl":%q,"apiKey":"secret-key","kind":%q}`, upstream.URL+"/v1", tc.kind)
			if tc.path == "/providers/probe" {
				body = fmt.Sprintf(`{"baseUrl":%q,"apiKey":"secret-key"}`, upstream.URL+"/v1")
			}
			resp := postProvider(t, srv.URL, tc.path, body)
			defer resp.Body.Close()
			var got struct {
				Code   string `json:"code"`
				Reason string `json:"reason"`
				Detail string `json:"detail"`
				Msg    string `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			host := strings.TrimPrefix(elsewhere.URL, "http://")
			switch tc.path {
			case "/providers/probe":
				if got.Code != "provider.probe.redirect_refused" || !strings.Contains(got.Msg, host) {
					t.Fatalf("probe refusal = %+v, want redirect_refused naming %s", got, host)
				}
			default:
				if got.Reason != "redirect_refused" || !strings.Contains(got.Detail, host) {
					t.Fatalf("model check = %+v, want redirect_refused naming %s", got, host)
				}
			}
			if n := reached.Load(); n != 0 {
				t.Fatalf("the redirect target received %d request(s); the key left the configured address", n)
			}
		})
	}
}
