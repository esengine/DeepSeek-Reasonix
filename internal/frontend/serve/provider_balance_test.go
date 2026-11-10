package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/model/billing"
	"reasonix/internal/session/control"
)

func editBalance(t *testing.T, srv *httptest.Server, fields string) (int, string) {
	t.Helper()
	resp := postProvider(t, srv.URL, "/providers/edit", `{"name":"rich","models":["alpha","beta"],"default":"alpha",`+fields+`}`)
	defer resp.Body.Close()
	body, _ := readAllString(resp)
	return resp.StatusCode, body
}

func readBalanceLedger(t *testing.T) string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(filepath.Dir(config.UserConfigPath()), "provider-keys.json"))
	return string(data)
}

func storedRich(t *testing.T) *config.ProviderEntry {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := cfg.Provider("rich")
	if !ok {
		t.Fatal("the provider disappeared")
	}
	return entry
}

// The wallet endpoint and its format are edited with the rest of the source,
// the format only from the declared set, and an address that is not http(s) is
// refused instead of being stored to fail on every status read.
func TestEditProviderSavesTheBalanceAddressAndFormat(t *testing.T) {
	srv := newRichProviderServer(t)

	if status, body := editBalance(t, srv, `"balanceUrl":"https://gateway.invalid/user/balance","balanceFormat":"moonshot"`); status != http.StatusNoContent {
		t.Fatalf("edit = %d: %s", status, body)
	}
	if e := storedRich(t); e.BalanceURL != "https://gateway.invalid/user/balance" || e.BalanceFormat != "moonshot" {
		t.Fatalf("stored %q %q, want the edited address and moonshot", e.BalanceURL, e.BalanceFormat)
	}

	if !strings.Contains(readBalanceLedger(t), `"format":"moonshot"`) {
		t.Fatalf("the saved format was not recorded for a rewrite to restore: %s", readBalanceLedger(t))
	}

	status, body := editBalance(t, srv, `"balanceFormat":"mystery"`)
	if status != http.StatusBadRequest || !strings.Contains(body, "provider.balance_format_unknown") {
		t.Fatalf("an unknown format = %d %s, want 400 provider.balance_format_unknown", status, body)
	}
	status, body = editBalance(t, srv, `"balanceUrl":"ftp://gateway.invalid/balance"`)
	if status != http.StatusBadRequest || !strings.Contains(body, "provider.balance_url_invalid") {
		t.Fatalf("a non-http address = %d %s, want 400 provider.balance_url_invalid", status, body)
	}
	if e := storedRich(t); e.BalanceFormat != "moonshot" {
		t.Fatalf("a refused edit changed the stored format to %q", e.BalanceFormat)
	}

	if status, body := editBalance(t, srv, `"balanceUrl":""`); status != http.StatusNoContent {
		t.Fatalf("clearing = %d: %s", status, body)
	}
	if e := storedRich(t); e.BalanceURL != "" || e.BalanceFormat != "" {
		t.Fatalf("clearing the address left %q %q behind", e.BalanceURL, e.BalanceFormat)
	}
	if strings.Contains(readBalanceLedger(t), "moonshot") {
		t.Fatalf("a cleared format is still recorded: %s", readBalanceLedger(t))
	}
}

func TestEditProviderLeavesTheWalletAloneWhenItIsNotSent(t *testing.T) {
	srv := newRichProviderServer(t)
	if status, body := editBalance(t, srv, `"balanceFormat":"deepseek"`); status != http.StatusNoContent {
		t.Fatalf("edit = %d: %s", status, body)
	}
	if status, body := editBalance(t, srv, `"contextWindow":64000`); status != http.StatusNoContent {
		t.Fatalf("edit = %d: %s", status, body)
	}
	if e := storedRich(t); e.BalanceURL != "https://gateway.invalid/balance" || e.BalanceFormat != "deepseek" {
		t.Fatalf("an unsent wallet changed: %q %q", e.BalanceURL, e.BalanceFormat)
	}
}

func TestProvidersListCarriesTheWalletAndWhatItsHostImplies(t *testing.T) {
	srv := newRichProviderServer(t)
	resp, err := http.Get(srv.URL + "/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var views []struct {
		Name                 string   `json:"name"`
		BalanceURL           string   `json:"balanceUrl"`
		BalanceFormat        string   `json:"balanceFormat"`
		ImpliedBalanceFormat string   `json:"impliedBalanceFormat"`
		BalanceFormats       []string `json:"balanceFormats"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&views); err != nil {
		t.Fatal(err)
	}
	var rich *struct {
		Name                 string   `json:"name"`
		BalanceURL           string   `json:"balanceUrl"`
		BalanceFormat        string   `json:"balanceFormat"`
		ImpliedBalanceFormat string   `json:"impliedBalanceFormat"`
		BalanceFormats       []string `json:"balanceFormats"`
	}
	for i := range views {
		if views[i].Name == "rich" {
			rich = &views[i]
		}
	}
	if rich == nil {
		t.Fatal("the provider is not listed")
	}
	if rich.BalanceURL != "https://gateway.invalid/balance" || rich.BalanceFormat != "" || rich.ImpliedBalanceFormat != "" {
		t.Fatalf("a relay's wallet = %+v, want its address, no declared format and none implied", rich)
	}
	if len(rich.BalanceFormats) != 3 {
		t.Fatalf("formats offered = %v, want the three declared ones", rich.BalanceFormats)
	}
}

// A relay with an address and no format reads as "not configured": the answer
// is a typed conflict, not a decode in somebody else's shape, and the endpoint
// is never asked, so the key is never sent to it.
func TestWalletOnAnUndeclaredHostSaysNotConfiguredAndNeverAsks(t *testing.T) {
	var hits atomic.Int64
	wallet := walletStub(t, &hits, nil)
	dir := testenv.TempDir(t)
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: dir, Balance: billing.NewCache(nil, wallet.URL, "", "")})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	t.Cleanup(srv.Close)
	status, _, refusal := readWallet(t, srv)
	if status != http.StatusConflict || refusal.Code != "wallet.format_not_declared" {
		t.Fatalf("wallet = %d %q, want 409 wallet.format_not_declared", status, refusal.Code)
	}
	if hits.Load() != 0 {
		t.Fatalf("the wallet endpoint was asked %d times without a declared format", hits.Load())
	}
}
