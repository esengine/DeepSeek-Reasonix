package providerhealth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
)

func TestConnectionAndWalletRemainIndependent(t *testing.T) {
	for _, tc := range []struct {
		status                      int
		wallet, connection, balance string
	}{
		{200, `{"is_available":false,"balance_infos":[{"currency":"CNY","total_balance":"0"}]}`, "reachable", "insufficient"},
		{401, `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"1"}]}`, "auth", "available"},
		{402, `{}`, "payment_required", "unknown"},
		{429, `{}`, "rate_limited", "unknown"},
		{503, `{}`, "unreachable", "unknown"},
		{404, `{}`, "unknown", "unknown"},
		{200, `{}`, "reachable", "unknown"},
		{429, `{"error":{"code":"insufficient_quota"}}`, "quota_exhausted", "unknown"},
		{429, `{"error":{"message":"insufficient_quota 余额不足"}}`, "rate_limited", "unknown"},
	} {
		t.Run(fmt.Sprint(tc.status, tc.balance), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("billable request: %s", r.Method)
				}
				if r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Error("missing saved credential")
				}
				if r.URL.Path == "/balance" {
					fmt.Fprint(w, tc.wallet)
					return
				}
				w.WriteHeader(tc.status)
				if tc.status == http.StatusOK {
					fmt.Fprint(w, `{"data":[]}`)
				} else {
					fmt.Fprint(w, tc.wallet)
				}
			}))
			defer srv.Close()
			p := config.ProviderEntry{Name: "test", Kind: "openai", BaseURL: srv.URL, BalanceURL: srv.URL + "/balance", BalanceFormat: "deepseek"}
			p = p.WithAPIKeyForProbe("test-secret")
			var checker Checker
			got := checker.Read(context.Background(), &p, netclient.ProxySpec{Mode: netclient.ModeOff})
			if got.Connection != tc.connection || got.Wallet != tc.balance || got.CheckedAt == 0 {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestCacheCoalescesAndInvalidatesOnCredentialsAndSettings(t *testing.T) {
	var hits atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			close(entered)
			<-release
		}
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer srv.Close()
	p := config.ProviderEntry{Name: "test", Kind: "openai", BaseURL: srv.URL}
	p = p.WithAPIKeyForProbe("first-secret")
	var checker Checker
	spec := netclient.ProxySpec{Mode: netclient.ModeOff}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { checker.Read(context.Background(), &p, spec) })
	}
	<-entered
	close(release)
	wg.Wait()
	if hits.Load() != 1 {
		t.Fatalf("duplicate checks: %d", hits.Load())
	}
	p = p.WithAPIKeyForProbe("second-secret")
	checker.Read(context.Background(), &p, spec)
	p.Headers = map[string]string{"X-Tenant": "other"}
	checker.Read(context.Background(), &p, spec)
	if hits.Load() != 3 {
		t.Fatalf("stale config check: %d", hits.Load())
	}
	for key, entry := range checker.entries {
		if len(key) != 32 || strings.Contains(fmt.Sprintf("%+v", entry), "secret") {
			t.Fatal("cache retained credentials")
		}
		entry.reading.CheckedAt = time.Now().Add(-2 * freshFor).UnixMilli()
	}
	checker.Read(t.Context(), &p, spec)
	if hits.Load() != 4 {
		t.Fatal("expired result did not refresh")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	checker.Read(ctx, &p, spec)
	if hits.Load() != 4 {
		t.Fatal("canceled read reached provider")
	}
}

func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	p := config.ProviderEntry{Name: "redirect", Kind: "openai", BaseURL: source.URL}
	p.BalanceURL, p.BalanceFormat = source.URL+"/balance", "deepseek"
	p = p.WithAPIKeyForProbe("secret")
	var checker Checker
	got := checker.Read(context.Background(), &p, netclient.ProxySpec{Mode: netclient.ModeOff})
	if got.Connection != "redirect_refused" || hits.Load() != 0 {
		t.Fatalf("got %+v, leaked requests %d", got, hits.Load())
	}
	source.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/secret" {
			hits.Add(1)
		}
		http.Redirect(w, r, "/secret", http.StatusFound)
	})
	p.Headers = map[string]string{"X-API-Key": "extra-secret"}
	got = checker.Read(t.Context(), &p, netclient.ProxySpec{Mode: netclient.ModeOff})
	raw, _ := json.Marshal(got)
	if hits.Load() != 0 || strings.Contains(string(raw), "secret") {
		t.Fatal("same-origin redirect followed or secret returned")
	}
}
