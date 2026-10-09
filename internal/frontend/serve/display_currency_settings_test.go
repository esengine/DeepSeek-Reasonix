package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func TestDisplayCurrencyReadsSavesAndRebindsTheLedger(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	if got := readJSON[control.DisplayCurrencySettings](t, srv.URL, "/display-currency"); got.Mode != "auto" {
		t.Fatalf("unset preference reads %q, want auto", got.Mode)
	}
	resp := postProvider(t, srv.URL, "/display-currency", `{"mode":"USD"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save = %d", resp.StatusCode)
	}
	if got := s.bc.DisplayCurrency(); got != "USD" {
		t.Fatalf("session ledger currency = %q, want USD", got)
	}
	if got := readJSON[control.DisplayCurrencySettings](t, srv.URL, "/display-currency"); got.Mode != "USD" {
		t.Fatalf("saved preference reads %q, want USD", got.Mode)
	}
	raw, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, keep := range []string{`default_model = "existing/model-a"`, `name = "existing"`, `api_key_env = "EXISTING_API_KEY"`} {
		if !strings.Contains(string(raw), keep) {
			t.Fatalf("saving the currency rewrote unrelated key %q:\n%s", keep, raw)
		}
	}

	resp = postProvider(t, srv.URL, "/display-currency", `{"mode":"auto"}`)
	resp.Body.Close()
	if got := s.bc.DisplayCurrency(); got != "" {
		t.Fatalf("auto left the ledger at %q", got)
	}
}

func TestDisplayCurrencyRefusesUnknownModeWithACode(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	bad := postProvider(t, srv.URL, "/display-currency", `{"mode":"EUR"}`)
	var refusal struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(bad.Body).Decode(&refusal)
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest || refusal.Code != "display_currency.invalid" {
		t.Fatalf("unknown mode = %d %q, want 400 display_currency.invalid", bad.StatusCode, refusal.Code)
	}
	if got := config.LoadForEdit(config.UserConfigPath()).DisplayCurrencyPref(); got != "" {
		t.Fatalf("a refused save reached the file: %q", got)
	}
}

func TestDisplayCurrencyReadsTheLegacyDesktopKeyAndAutoClearsBoth(t *testing.T) {
	s := newProviderEditServer(t)
	path := config.UserConfigPath()
	body, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(body, []byte("\n[desktop]\ncurrency = \"CNY\"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	if got := readJSON[control.DisplayCurrencySettings](t, srv.URL, "/display-currency"); got.Mode != "CNY" {
		t.Fatalf("legacy [desktop].currency reads %q, want CNY", got.Mode)
	}
	resp := postProvider(t, srv.URL, "/display-currency", `{"mode":"auto"}`)
	resp.Body.Close()
	cfg := config.LoadForEdit(path)
	if cfg.DisplayCurrencyPref() != "" {
		t.Fatalf("auto left the legacy key behind: %q", cfg.DisplayCurrencyPref())
	}
}
