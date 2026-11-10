package boot

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/model/billing"
)

const bootWalletJSON = `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"42.00","granted_balance":"0","topped_up_balance":"42.00"}]}`

func buildWithWallet(t *testing.T, balanceFormat string) (*atomic.Int64, billing.Reading) {
	t.Helper()
	hits := new(atomic.Int64)
	wallet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, bootWalletJSON)
	}))
	t.Cleanup(wallet.Close)

	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	kind := "boot-wallet-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + balanceFormat + "-fmt"
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return &peerRecorder{}, nil })
	format := ""
	if balanceFormat != "" {
		format = "balance_format = \"" + balanceFormat + "\"\n"
	}
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
balance_url = "`+wallet.URL+`/user/balance"
`+format)
	approveWorkspaceAt(t, home, dir)
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)
	return hits, ctrl.Balance(context.Background())
}

// A wallet address that is not a vendor's own host is read in the shape its
// entry declares: the declared one reaches the status line, and with none the
// endpoint is not asked at all.
func TestEffectBalanceFormatDecidesWhetherARelayWalletIsRead(t *testing.T) {
	hits, reading := buildWithWallet(t, "deepseek")
	if reading.Balance == nil || reading.Balance.Display() != "¥42.00" {
		t.Fatalf("a declared format on a relay: %+v, want ¥42.00", reading)
	}
	if hits.Load() == 0 {
		t.Fatal("the declared wallet was never asked")
	}

	hits, reading = buildWithWallet(t, "")
	if reading.Balance != nil || reading.Err == nil || !strings.Contains(reading.Err.Error(), "not declared") {
		t.Fatalf("no format on a relay: %+v, want not declared", reading)
	}
	if hits.Load() != 0 {
		t.Fatalf("a relay with no format was asked %d times: the key would have been sent to it", hits.Load())
	}
}
