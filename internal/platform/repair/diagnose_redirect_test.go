package repair

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/config"
)

func TestProbeProviderNetworkDoesNotFollowARedirectWithTheKey(t *testing.T) {
	var reached atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
	defer elsewhere.Close()
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	defer home.Close()
	t.Setenv("DOCTOR_PROBE_KEY", "secret")
	cfg := &config.Config{Providers: []config.ProviderEntry{{Name: "p", Kind: "openai", BaseURL: home.URL + "/v1", APIKeyEnv: "DOCTOR_PROBE_KEY", Headers: map[string]string{"x-extra": "1"}}}}
	var report DiagnosticReport
	probeProviderNetwork(t.Context(), &report, cfg, 5*time.Second)
	if n := reached.Load(); n != 0 {
		t.Fatalf("redirect target received %d request(s)", n)
	}
	for _, f := range report.Findings {
		if f.Code == "network.redirect_refused" {
			return
		}
	}
	t.Fatalf("findings = %+v, want network.redirect_refused", report.Findings)
}
