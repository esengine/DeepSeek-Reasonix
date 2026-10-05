package pricing

import (
	"strings"
	"testing"
	"time"
)

// Every catalog provider must declare at least one official endpoint, or its
// rows become unreachable: a rate card is only matched against the catalog
// after the entry's host identifies the vendor.
func TestEveryCatalogProviderHasAnOfficialEndpoint(t *testing.T) {
	declared := map[string]bool{}
	for _, host := range officialEndpointHosts {
		declared[host] = true
	}
	for _, entry := range OfficialCatalog() {
		if !declared[entry.Provider] {
			t.Errorf("catalog provider %q has no official endpoint host; its prices can never match", entry.Provider)
		}
	}
}

func TestOfficialProviderForEndpoint(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"official deepseek", "https://api.deepseek.com", "deepseek"},
		{"official deepseek anthropic path", "https://api.deepseek.com/anthropic", "deepseek"},
		{"official longcat", "https://api.longcat.chat/openai/v1", "longcat"},
		{"official mimo region", "https://token-plan-sgp.xiaomimimo.com/v1", "mimo"},
		{"uppercase host", "https://API.DeepSeek.com/v1", "deepseek"},
		{"proxy that resells the model", "https://my-deepseek-proxy.internal/v1", ""},
		{"vendor name in the path only", "https://gateway.example.com/deepseek/v1", ""},
		{"lookalike registrable domain", "https://api.deepseek.com.evil.test/v1", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := OfficialProviderForEndpoint(tc.baseURL); got != tc.want {
				t.Fatalf("OfficialProviderForEndpoint(%q) = %q, want %q", tc.baseURL, got, tc.want)
			}
		})
	}
}

// pricesGoStaleAfter is how long a hand-copied rate is trusted before someone
// reads the vendor's page again. No vendor here serves prices over an API, so
// the alternative to this going red on a quiet day is learning about a change
// from a user whose cost was wrong for months. Bumping CheckedOn without
// opening DocURL defeats it; what this buys is that the question gets asked.
const pricesGoStaleAfter = 90 * 24 * time.Hour

func TestCatalogPricesHaveBeenCheckedRecently(t *testing.T) {
	var undated, stale []string
	cutoff := time.Now().UTC().Add(-pricesGoStaleAfter)
	for _, e := range OfficialCatalog() {
		where := e.Provider + "/" + e.Model + " " + e.Currency + " (" + e.DocURL + ")"
		if e.CheckedOn == "" {
			undated = append(undated, where)
			continue
		}
		on, err := time.Parse("2006-01-02", e.CheckedOn)
		if err != nil {
			t.Fatalf("%s: CheckedOn %q is not YYYY-MM-DD", where, e.CheckedOn)
		}
		if on.Before(cutoff) {
			stale = append(stale, where+" last checked "+e.CheckedOn)
		}
	}
	if len(undated) > 0 {
		t.Errorf("catalog rows with no CheckedOn — read the vendor's page and date them:\n  %s",
			strings.Join(undated, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("catalog rows older than %d days. Open each DocURL, confirm or correct the rate, then set CheckedOn to today:\n  %s",
			int(pricesGoStaleAfter.Hours()/24), strings.Join(stale, "\n  "))
	}
}

// A row that names a peak card must say when it applies, and one that names a
// window must have a card to charge inside it. Half of the pair silently prices
// every hour the same, which is the bug the pair exists to prevent.
func TestPeakRatesAndWindowsComeInPairs(t *testing.T) {
	for _, e := range OfficialCatalog() {
		where := e.Provider + "/" + e.Model + " " + e.Currency
		if (e.Peak == nil) != (e.Window == nil) {
			t.Errorf("%s: Peak and Window must be set together (peak=%v window=%v)", where, e.Peak != nil, e.Window != nil)
		}
	}
}
