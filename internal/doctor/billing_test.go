package doctor

import (
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestCollectBillingIncludesProvidersAndDisplay(t *testing.T) {
	cfg := config.Default()
	cfg.Billing.DisplayCurrency = "CNY"
	rep := CollectBilling(cfg)
	if rep.DisplayCurrency != "CNY" {
		t.Fatalf("display = %q", rep.DisplayCurrency)
	}
	if len(rep.Providers) == 0 {
		t.Fatal("expected providers")
	}
	text := RenderBillingText(rep)
	for _, want := range []string{"display preference", "display resolved", "providers:", "fx enabled:         false", "Runtime FX is disabled"} {
		if !strings.Contains(text, want) {
			t.Fatalf("render missing %q:\n%s", want, text)
		}
	}
}
