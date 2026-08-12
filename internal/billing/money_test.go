package billing

import (
	"testing"
)

func TestAmountRoundTrip(t *testing.T) {
	a, err := NewAmountFromString("1.23456789")
	if err != nil {
		t.Fatal(err)
	}
	if got := a.String(); got != "1.23456789" {
		t.Fatalf("String = %q", got)
	}
	if a.Float64() < 1.23456788 || a.Float64() > 1.23456790 {
		t.Fatalf("Float64 = %v", a.Float64())
	}
}

func TestAddMoneyRejectsMixedCurrency(t *testing.T) {
	_, err := AddMoney(MoneyOf(NewAmountFromFloat(1), "CNY"), MoneyOf(NewAmountFromFloat(1), "USD"))
	if err == nil {
		t.Fatal("expected mixed currency error")
	}
}

func TestAddMoneySameCurrency(t *testing.T) {
	sum, err := AddMoney(MoneyOf(NewAmountFromFloat(1.5), "USD"), MoneyOf(NewAmountFromFloat(2.25), "USD"))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Currency != "USD" || sum.Amount != "3.75" {
		t.Fatalf("sum = %+v", sum)
	}
}

func TestNormalizeCurrency(t *testing.T) {
	cases := map[string]string{
		"¥": "CNY", "$": "USD", "rmb": "CNY", "usd": "USD", "eur": "EUR",
	}
	for in, want := range cases {
		if got := NormalizeCurrency(in); got != want {
			t.Errorf("NormalizeCurrency(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOriginalCostMatchesLegacySemantics(t *testing.T) {
	// Same as provider.Pricing.Cost cache-write split test: miss 500k, write 100k billed 200k, input rate 2 → 1.2
	amt := OriginalCostAmount(RateCard{Input: 2, Currency: "USD"}, UsageTokens{
		CacheMissTokens: 500_000, CacheWriteTokens: 100_000, CacheWriteBilledTokens: 200_000,
	})
	if got := amt.Float64(); got < 1.199 || got > 1.201 {
		t.Fatalf("cost = %v, want ~1.2", got)
	}
}
