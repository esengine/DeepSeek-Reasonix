package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/pricing"
)

// The footer's spend says which side of the vendor's peak window it was billed
// on, as 1.x does; a total with no band shows the amount alone.
func TestFooterSpendNamesItsRateBand(t *testing.T) {
	m, _ := testModel(t)
	cost := func(band string) string {
		m.status.SessionCostQuote = &CostQuote{Original: Money{Amount: "0.0123", Currency: "CNY"}, CostComplete: true, RateBand: band}
		return strings.Join(footerPlainGroups(m), " ")
	}
	for band, want := range map[string]string{
		pricing.RateBandPeak: i18n.M.RateBandPeak, pricing.RateBandOffPeak: i18n.M.RateBandOffPeak, pricing.RateBandMixed: i18n.M.RateBandMixed,
	} {
		if got := cost(band); !strings.Contains(got, "≈¥0.0123 · "+want) {
			t.Errorf("band %q: footer %q lacks %q", band, got, want)
		}
	}
	if got := cost(""); !strings.Contains(got, "≈¥0.0123") || strings.Contains(got, "0.0123 ·") {
		t.Errorf("a total with no band shows %q", got)
	}
}

func TestTurnReceiptNamesItsRateBand(t *testing.T) {
	u := &eventwire.Usage{TotalTokens: 1200, PromptTokens: 1000, CompletionTokens: 200, Cost: 0.5, CurrencyCode: "CNY",
		CostQuote: &pricing.CostQuote{RateBand: pricing.RateBandPeak}}
	if got := ansi.Strip(renderUsage(u, 100)); !strings.Contains(got, "≈¥0.5000 · "+i18n.M.RateBandPeak) {
		t.Fatalf("receipt lacks the band:\n%s", got)
	}
}
