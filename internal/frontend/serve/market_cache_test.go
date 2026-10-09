package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/market"
)

type hostRewrite struct {
	to   string
	base http.RoundTripper
}

func (h hostRewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Host = h.to
	return h.base.RoundTrip(r)
}

func realRegistryAt(t *testing.T, handler http.HandlerFunc) *atomic.Bool {
	t.Helper()
	var down atomic.Bool
	reg := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(reg.Close)
	u, _ := url.Parse(reg.URL)
	hc := &http.Client{Transport: hostRewrite{to: u.Host, base: reg.Client().Transport}}
	oldReg, oldBrowse := marketRegistry, marketBrowser
	marketRegistry = func(*http.Client) market.Registry { return market.NewClient(hc) }
	marketBrowser = func(*http.Client) market.Registry { return market.NewClient(hc).WithCache(config.CacheDir()) }
	t.Cleanup(func() { marketRegistry, marketBrowser = oldReg, oldBrowse })
	return &down
}

func marketGetJSON(t *testing.T, u string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestMarketBrowseAnswersFromTheLastGoodCopyAndLabelsIt(t *testing.T) {
	_, _, base := pluginHome(t)
	down := realRegistryAt(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/packages/a/") {
			_, _ = w.Write([]byte(`{"package":{"slug":"a/kit","status":"active","latestVersion":"1.0.0"},"versions":[{"version":"1.0.0","source":"https://example.test/SKILL.md","content_hash":"sha256:abababababababababababababababababababababababababababababababab"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"packages":[{"slug":"a/kit","status":"active","name":"kit"}],"limit":24,"offset":0}`))
	})

	code, fresh := marketGetJSON(t, base+"/market/packages?q=kit")
	if code != 200 || fresh["cache"] != nil {
		t.Fatalf("fresh list = %d %v", code, fresh)
	}
	if code, _ = marketGetJSON(t, base+"/market/packages/a/kit"); code != 200 {
		t.Fatalf("detail = %d", code)
	}

	down.Store(true)
	if code, again := marketGetJSON(t, base+"/market/packages?q=kit"); code != 200 || again["cache"] != nil {
		t.Fatalf("inside the fresh window the copy is the answer, unlabelled: %d %v", code, again)
	}
	code, list := marketGetJSON(t, base+"/market/packages?q=kit&refresh=1")
	note, _ := list["cache"].(map[string]any)
	if code != 200 || note == nil || note["cause"] != "bad_response" || note["cachedAt"] == "" {
		t.Fatalf("stale list = %d %v", code, list)
	}
	if rows, _ := list["packages"].([]any); len(rows) != 1 {
		t.Fatalf("rows = %v", list["packages"])
	}
	code, detail := marketGetJSON(t, base+"/market/packages/a/kit?refresh=1")
	if dn, _ := detail["cache"].(map[string]any); code != 200 || dn == nil || detail["pinned"] != true {
		t.Fatalf("stale detail = %d %v", code, detail)
	}

	code, other := marketGetJSON(t, base+"/market/packages?q=never-asked")
	if code != http.StatusBadGateway || other["code"] != "market.bad_response" {
		t.Fatalf("an unseen query must still fail loudly: %d %v", code, other)
	}
}

func TestMarketInstallNeverReadsTheBrowseCache(t *testing.T) {
	_, _, base := pluginHome(t)
	down := realRegistryAt(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"package":{"slug":"a/kit","status":"active","latestVersion":"1.0.0"},"versions":[{"version":"1.0.0","source":"https://example.test/SKILL.md","content_hash":"sha256:abababababababababababababababababababababababababababababababab"}]}`))
	})
	if code, _ := marketGetJSON(t, base+"/market/packages/a/kit"); code != 200 {
		t.Fatalf("detail = %d", code)
	}
	down.Store(true)
	resp, err := http.Post(base+"/market/plan", "application/json", strings.NewReader(`{"slug":"a/kit"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := marketCode(t, resp); got != "market.bad_response" {
		t.Fatalf("plan code = %q, want the registry failure, not a cached detail", got)
	}
}
