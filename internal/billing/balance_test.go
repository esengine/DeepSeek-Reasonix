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
