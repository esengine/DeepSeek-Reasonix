package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/model/billing"
	"reasonix/internal/session/control"
)

const walletJSON = `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"10.00","granted_balance":"0","topped_up_balance":"10.00"}]}`

// walletStub is a balance endpoint that counts what reaches it.
func walletStub(t *testing.T, hits *atomic.Int64, status *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		if status != nil && status.Load() != 0 {
			w.WriteHeader(int(status.Load()))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, walletJSON)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func walletServer(t *testing.T, balanceURL string) *httptest.Server {
	t.Helper()
	dir := testenv.TempDir(t)
	active := filepath.Join(dir, "20260101-000000.000000000-status.jsonl")
	if err := os.WriteFile(active, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: dir, SessionPath: active, Balance: billing.NewCache(nil, balanceURL, "")})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv
}

func readStatus(t *testing.T, srv *httptest.Server) map[string]any {
	t.Helper()
	resp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func readWallet(t *testing.T, srv *httptest.Server) (int, walletBody, Reason) {
	t.Helper()
	resp, err := http.Get(srv.URL + "/balance")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body walletBody
	var refusal Reason
	raw, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
	case http.StatusNoContent:
	default:
		if err := json.Unmarshal(raw, &refusal); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, body, refusal
}

// The effect this route exists for: /status is polled four times a second while
// a turn runs, and the wallet no longer rides it. One read first, so what this
// counts is the polling rather than a wallet nobody had asked for yet.
func TestStatusPollingNeverTouchesTheWallet(t *testing.T) {
	var hits atomic.Int64
	wallet := walletStub(t, &hits, nil)
	srv := walletServer(t, wallet.URL)

	readWallet(t, srv)
	settled := hits.Load()
	if settled != 1 {
		t.Fatalf("the first wallet read cost %d requests, want 1", settled)
	}
	// Five seconds of the desktop's 250ms poll.
	for range 20 {
		if _, ok := readStatus(t, srv)["balance"]; ok {
			t.Fatal("/status still carries a balance")
		}
	}
	if got := hits.Load(); got != settled {
		t.Fatalf("status polling cost %d wallet requests", got-settled)
	}
}

func TestWalletReadsShareOneFetch(t *testing.T) {
	var hits atomic.Int64
	wallet := walletStub(t, &hits, nil)
	srv := walletServer(t, wallet.URL)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { readWallet(t, srv) })
	}
	wg.Wait()
	if got := hits.Load(); got != 1 {
		t.Fatalf("wallet requests = %d, want 1", got)
	}
	status, body, _ := readWallet(t, srv)
	if status != http.StatusOK || body.Display != "¥10.00" {
		t.Fatalf("read %d %+v", status, body)
	}
	if body.Stale {
		t.Error("a wallet read a moment ago reported itself as standing in")
	}
}

// A provider with no wallet is an absence to render as nothing. 204 says so
// without making the frontend read a failure as one.
func TestWalletWithoutEndpointIsAbsent(t *testing.T) {
	srv := walletServer(t, "")
	if status, _, _ := readWallet(t, srv); status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", status)
	}
}

// A refused key and an endpoint having a bad moment are different things to do
// next, so the refusal carries which one rather than one sentence for both.
func TestWalletFailureCarriesItsCode(t *testing.T) {
	for _, tc := range []struct {
		status int64
		want   string
	}{
		{http.StatusUnauthorized, "wallet.unauthorized"},
		{http.StatusInternalServerError, "wallet.unreachable"},
	} {
		var hits atomic.Int64
		var code atomic.Int64
		code.Store(tc.status)
		wallet := walletStub(t, &hits, &code)
		srv := walletServer(t, wallet.URL)
		status, _, refusal := readWallet(t, srv)
		if status != http.StatusBadGateway || refusal.Code != tc.want {
			t.Errorf("wallet %d gave %d %q, want 502 %q", tc.status, status, refusal.Code, tc.want)
		}
	}
}
