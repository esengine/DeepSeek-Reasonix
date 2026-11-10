package boot

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

var tariffKinds atomic.Int64

func tariffOf(t *testing.T, baseURL string, price string) bool {
	t.Helper()
	home := statedBootHome(t)
	dir := testenv.TempDir(t)
	kind := fmt.Sprintf("boot-tariff-%s-%d", strings.ReplaceAll(t.Name(), "/", "-"), tariffKinds.Add(1))
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return &peerRecorder{}, nil })
	body := `
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "` + kind + `"
base_url = "` + baseURL + `"
model = "deepseek-flash"
` + price
	writeFile(t, dir, "reasonix.toml", body)
	approveWorkspaceAt(t, home, dir)
	ctrl, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)
	_, ok := ctrl.Tariff()
	return ok
}

// Through the real assembly: the vendor's endpoint at its list price is billed
// under the vendor's schedule; the same model name on a relay, or at a price the
// user typed, is not.
func TestEffectTariffAppliesOnlyToTheVendorsOwnRate(t *testing.T) {
	const flashPrice = "\n[providers.prices.\"deepseek-flash\"]\ninput = 1.0\noutput = 4.0\ncache_hit = 0.02\ncurrency = \"CNY\"\n"
	if !tariffOf(t, "https://api.deepseek.com/anthropic", flashPrice) {
		t.Fatal("the vendor's own endpoint at its list price has no schedule")
	}
	if tariffOf(t, "https://relay.example/v1", flashPrice) {
		t.Fatal("a relay was given the vendor's schedule")
	}
	custom := strings.Replace(flashPrice, "input = 1.0", "input = 7.5", 1)
	if tariffOf(t, "https://api.deepseek.com/anthropic", custom) {
		t.Fatal("a price the user typed was given the vendor's schedule")
	}
}
