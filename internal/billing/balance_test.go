package billing

import "testing"

func TestBalanceOriginalCurrencyDisplayNeverConverts(t *testing.T) {
	b := &Balance{Available: true, Infos: []Info{
		{Currency: "CNY", TotalBalance: "70.16"},
		{Currency: "USD", TotalBalance: "9.82"},
	}}
	if got := b.DisplayForCurrency("USD"); got != "$9.82" {
		t.Fatalf("USD display = %q", got)
	}
	if got := (&Balance{Infos: []Info{{Currency: "CNY", TotalBalance: "70.16"}}}).DisplayForCurrency("USD"); got != "CNY ¥70.16" {
		t.Fatalf("fallback display = %q", got)
	}
	if got := b.PrimaryCurrency(); got != "" || !b.MultiCurrency() {
		t.Fatalf("wallet currencies = %v primary=%q", b.Currencies(), got)
	}
}

func TestBalanceSingleWalletCurrencyHint(t *testing.T) {
	b := &Balance{Infos: []Info{{Currency: "RMB", TotalBalance: "1"}}}
	if got := b.PrimaryCurrency(); got != "CNY" {
		t.Fatalf("primary = %q", got)
	}
	if got := b.Currencies(); len(got) != 1 || got[0] != "CNY" {
		t.Fatalf("currencies = %v", got)
	}
}

func TestDisplayFallbackPrefersFundedBalance(t *testing.T) {
	// DeepSeek returns a zero CNY entry alongside the funded USD entry for
	// USD-funded accounts, in non-deterministic order (see #8107). The
	// fallback must show the funded currency, never the zero CNY entry.
	usdFunded := Info{Currency: "USD", TotalBalance: "9.82"}
	cnyZero := Info{Currency: "CNY", TotalBalance: "0.00"}
	for _, infos := range [][]Info{
		{cnyZero, usdFunded},
		{usdFunded, cnyZero},
	} {
		if got := (&Balance{Infos: infos}).DisplayForCurrency(""); got != "$9.82" {
			t.Fatalf("fallback display for %v = %q, want $9.82", infos, got)
		}
	}
	// An explicit preference still wins even when its own balance is zero.
	if got := (&Balance{Infos: []Info{cnyZero, usdFunded}}).DisplayForCurrency("CNY"); got != "¥0.00" {
		t.Fatalf("explicit CNY display = %q, want ¥0.00", got)
	}
	// A USD-only wallet shows USD for every preference; unknown preferences
	// are normalized to empty and render without a prefix.
	usdOnly := &Balance{Infos: []Info{usdFunded}}
	for pref, want := range map[string]string{
		"":    "$9.82",
		"USD": "$9.82",
		"JPY": "$9.82",
		"CNY": "USD $9.82",
	} {
		if got := usdOnly.DisplayForCurrency(pref); got != want {
			t.Fatalf("USD-only display for %q = %q, want %q", pref, got, want)
		}
	}
	// Malformed totals never win the fallback pick.
	if got := (&Balance{Infos: []Info{{Currency: "CNY", TotalBalance: "oops"}, usdFunded}}).DisplayForCurrency(""); got != "$9.82" {
		t.Fatalf("malformed-total fallback = %q, want $9.82", got)
	}
}
